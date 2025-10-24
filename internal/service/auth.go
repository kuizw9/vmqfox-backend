package service

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	"vmqfox-api-go/internal/model"
	"vmqfox-api-go/internal/repository"
	"vmqfox-api-go/pkg/jwt"

	"gorm.io/gorm"
)

// 认证相关错误
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user is disabled")
	ErrInvalidToken       = errors.New("invalid token")
)

// 注册频率限制
type rateLimitEntry struct {
	count     int
	resetTime time.Time
}

var (
	rateLimitMap = make(map[string]*rateLimitEntry)
	rateLimitMux = sync.RWMutex{}
)

// AuthService 认证服务接口
type AuthService interface {
	Login(req *model.LoginRequest) (*model.LoginResponse, error)
	Register(req *model.RegisterRequest, clientIP string) (*model.RegisterResponse, error)
	RefreshToken(refreshToken string) (*model.LoginResponse, error)
	GetCurrentUser(userID uint) (*model.SafeUser, error)
	Logout(userID uint) error
}

// authService 认证服务实现
type authService struct {
	userRepo           repository.UserRepository
	globalSettingRepo  repository.GlobalSettingRepository
	jwtManager         *jwt.JWTManager
}

// NewAuthService 创建认证服务
func NewAuthService(userRepo repository.UserRepository, globalSettingRepo repository.GlobalSettingRepository, jwtManager *jwt.JWTManager) AuthService {
	return &authService{
		userRepo:          userRepo,
		globalSettingRepo: globalSettingRepo,
		jwtManager:        jwtManager,
	}
}

// Login 用户登录
func (s *authService) Login(req *model.LoginRequest) (*model.LoginResponse, error) {
	// 查找用户
	user, err := s.userRepo.GetByUsername(req.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// 验证密码
	if !user.VerifyPassword(req.Password) {
		return nil, ErrInvalidCredentials
	}

	// 检查用户状态
	if !user.IsEnabled() {
		return nil, ErrUserDisabled
	}

	// 生成JWT令牌
	accessToken, refreshToken, err := s.jwtManager.GenerateTokens(
		user.Id,
		user.Username,
		user.Role,
		user.Status,
	)
	if err != nil {
		return nil, err
	}

	// 返回登录响应
	return &model.LoginResponse{
		User:          user,
		Access_token:  accessToken,
		Refresh_token: refreshToken,
		Expires_in:    7200, // 2小时，应该从配置读取
	}, nil
}

// getRegisterConfig 获取注册配置
func (s *authService) getRegisterConfig() (*model.RegisterConfig, error) {
	// 从global_settings表获取配置
	config, err := s.globalSettingRepo.GetRegisterConfig()
	if err != nil {
		return nil, err
	}

	// 验证默认角色是否有效
	if !isValidRole(config.DefaultRole) {
		config.DefaultRole = model.RoleAdmin
	}

	return config, nil
}

// isValidRole 验证角色是否有效
func isValidRole(role string) bool {
	return role == model.RoleAdmin || role == model.RoleSuperAdmin
}

// checkRateLimit 检查注册频率限制
func (s *authService) checkRateLimit(clientIP string, limit int) error {
	rateLimitMux.Lock()
	defer rateLimitMux.Unlock()

	now := time.Now()
	entry, exists := rateLimitMap[clientIP]

	if !exists {
		// 首次注册
		rateLimitMap[clientIP] = &rateLimitEntry{
			count:     1,
			resetTime: now.Add(time.Hour),
		}
		return nil
	}

	// 检查是否需要重置计数器
	if now.After(entry.resetTime) {
		entry.count = 1
		entry.resetTime = now.Add(time.Hour)
		return nil
	}

	// 检查是否超过限制
	if entry.count >= limit {
		return errors.New("注册频率过高，请稍后再试")
	}

	// 增加计数
	entry.count++
	return nil
}

// Register 用户注册
func (s *authService) Register(req *model.RegisterRequest, clientIP string) (*model.RegisterResponse, error) {
	// 获取注册配置
	config, err := s.getRegisterConfig()
	if err != nil {
		return nil, err
	}

	// 检查是否开放注册
	if !config.Enabled {
		return nil, errors.New("用户注册功能已关闭")
	}

	// 检查注册频率限制
	if err := s.checkRateLimit(clientIP, config.RateLimit); err != nil {
		return nil, err
	}

	// 验证请求数据
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// 检查用户名是否已存在
	exists, err := s.userRepo.ExistsByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("用户名已存在")
	}

	// 检查邮箱是否已存在
	exists, err = s.userRepo.ExistsByEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("邮箱已存在")
	}

	// 创建用户对象
	user := &model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,        // 密码会在BeforeCreate钩子中自动加密
		Role:     config.DefaultRole,  // 使用配置中的默认角色
		Status:   model.StatusEnabled, // 默认启用
	}

	// 保存用户
	if err := s.userRepo.Create(user); err != nil {
		return nil, err
	}

	// 为新用户创建默认设置
	if err := s.createUserSettings(user); err != nil {
		// 记录错误并返回错误，因为这是关键功能
		log.Printf("Error: Failed to create default settings for user %d: %v", user.Id, err)
		return nil, fmt.Errorf("failed to create default settings: %v", err)
	}

	// 返回注册响应（不包含敏感信息）
	return &model.RegisterResponse{
		Message: "注册成功",
		User:    user,
	}, nil
}

// RefreshToken 刷新令牌
func (s *authService) RefreshToken(refreshToken string) (*model.LoginResponse, error) {
	// 验证刷新令牌
	claims, err := s.jwtManager.ValidateToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidToken
	}

	// 检查令牌类型
	if claims.Type != "refresh" {
		return nil, ErrInvalidToken
	}

	// 获取用户信息（确保用户仍然存在且启用）
	user, err := s.userRepo.GetByID(claims.UserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// 检查用户状态
	if !user.IsEnabled() {
		return nil, ErrUserDisabled
	}

	// 生成新的令牌对
	accessToken, newRefreshToken, err := s.jwtManager.GenerateTokens(
		user.Id,
		user.Username,
		user.Role,
		user.Status,
	)
	if err != nil {
		return nil, err
	}

	// 返回新的令牌
	return &model.LoginResponse{
		User:          user,
		Access_token:  accessToken,
		Refresh_token: newRefreshToken,
		Expires_in:    7200, // 2小时，应该从配置读取
	}, nil
}

// GetCurrentUser 获取当前用户信息
func (s *authService) GetCurrentUser(userID uint) (*model.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	return user, nil
}

// Logout 用户注销
func (s *authService) Logout(userID uint) error {
	// 在实际应用中，这里可能需要：
	// 1. 将令牌加入黑名单
	// 2. 记录注销日志
	// 3. 清理相关缓存

	// 目前只是一个占位符实现
	// 由于JWT是无状态的，客户端删除令牌即可实现注销
	return nil
}

// createUserSettings 为新用户创建完整的配置数据（已废弃，配置已移至users表）
func (s *authService) createUserSettings(user *model.User) error {
	// 配置已在UserService.CreateUser中处理，这里不需要再创建
	log.Printf("User settings are now managed in users table for user %s (ID: %d)", user.Username, user.Id)
	return nil
}

// generateAppID 生成唯一的AppID（已废弃，AppID在UserService中生成）
func (s *authService) generateAppID() (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const maxAttempts = 10

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// 生成12位随机字符串
		randomBytes := make([]byte, 12)
		for i := range randomBytes {
			randomBytes[i] = charset[time.Now().UnixNano()%int64(len(charset))]
		}

		appID := "VMQ_" + string(randomBytes)

		// 检查AppID是否已存在
		_, err := s.userRepo.GetByAppID(appID)
		if err != nil {
			// AppID不存在，可以使用
			return appID, nil
		}

		// AppID已存在，继续尝试
		if attempt < maxAttempts-1 {
			return appID, nil
		}

		// 如果存在，稍微延迟后重试
		time.Sleep(time.Millisecond)
	}

	return "", errors.New("failed to generate unique AppID after multiple attempts")
}
