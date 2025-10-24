package service

import (
	"crypto/md5"
	"errors"
	"fmt"
	"time"

	"vmqfox-api-go/internal/model"
	"vmqfox-api-go/internal/repository"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// 错误定义
var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")
)

// UserService 用户服务接口
type UserService interface {
	GetUsers(page, limit int, search string) ([]*model.User, int64, error)
	GetUserByID(id uint) (*model.User, error)
	GetUserByAppID(appId string) (*model.User, error)
	CreateUser(req *model.CreateUserRequest) (*model.User, error)
	UpdateUser(id uint, req *model.UpdateUserRequest) (*model.User, error)
	DeleteUser(id uint) error
	ResetPassword(id uint, password string) error
	UpdateUserConfig(userID uint, config map[string]interface{}) error
}

// userService 用户服务实现
type userService struct {
	userRepo repository.UserRepository
}

// NewUserService 创建用户服务
func NewUserService(userRepo repository.UserRepository) UserService {
	return &userService{
		userRepo: userRepo,
	}
}

// GetUsers 获取用户列表
func (s *userService) GetUsers(page, limit int, search string) ([]*model.User, int64, error) {
	return s.userRepo.GetUsers(page, limit, search)
}

// GetUserByID 根据ID获取用户
func (s *userService) GetUserByID(id uint) (*model.User, error) {
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

// CreateUser 创建用户
func (s *userService) CreateUser(req *model.CreateUserRequest) (*model.User, error) {
	// 检查用户名是否已存在
	exists, err := s.userRepo.ExistsByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrUserExists
	}

	// 检查邮箱是否已存在
	exists, err = s.userRepo.ExistsByEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrUserExists
	}

	// 创建用户对象
	user := &model.User{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password, // 密码会在BeforeCreate钩子中自动加密
		Role:     req.Role,
	}

	// 设置默认角色
	if user.Role == "" {
		user.Role = model.RoleAdmin
	}

	// 设置默认状态为启用（保持数据库兼容性）
	user.Status = model.StatusEnabled

	// 生成默认配置
	keyValue := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%d", time.Now().Unix()))))
	appIdValue := fmt.Sprintf("vmqfox_%d", time.Now().Unix())

	user.Key = &keyValue
	user.AppId = &appIdValue

	// 设置默认值
	defaultClose := 5
	defaultPayQf := 1
	defaultJkstate := 0
	user.Close = &defaultClose
	user.PayQf = &defaultPayQf
	user.Jkstate = &defaultJkstate

	// 保存用户
	if err := s.userRepo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateUser 更新用户
func (s *userService) UpdateUser(id uint, req *model.UpdateUserRequest) (*model.User, error) {
	// 获取现有用户
	user, err := s.userRepo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// 检查用户名是否已被其他用户使用
	if req.Username != "" && req.Username != user.Username {
		exists, err := s.userRepo.ExistsByUsernameExcludeID(req.Username, id)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrUserExists
		}
		user.Username = req.Username
	}

	// 检查邮箱是否已被其他用户使用
	if req.Email != "" && req.Email != user.Email {
		exists, err := s.userRepo.ExistsByEmailExcludeID(req.Email, id)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrUserExists
		}
		user.Email = req.Email
	}

	// 更新角色
	if req.Role != "" {
		user.Role = req.Role
	}

	// 保存更新
	if err := s.userRepo.Update(user); err != nil {
		return nil, err
	}

	return user, nil
}

// DeleteUser 删除用户
func (s *userService) DeleteUser(id uint) error {
	// 检查用户是否存在
	_, err := s.userRepo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	// 删除用户（外键约束会自动删除关联的订单、收款码等）
	return s.userRepo.Delete(id)
}

// ResetPassword 重置密码
func (s *userService) ResetPassword(id uint, password string) error {
	// 获取用户
	_, err := s.userRepo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 直接更新密码，避免BeforeUpdate钩子的重复加密
	return s.userRepo.UpdatePasswordDirect(id, string(hashedPassword))
}

// GetUserByAppID 根据AppID获取用户
func (s *userService) GetUserByAppID(appId string) (*model.User, error) {
	user, err := s.userRepo.GetByAppID(appId)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

// UpdateUserConfig 更新用户配置
func (s *userService) UpdateUserConfig(userID uint, config map[string]interface{}) error {
	// 获取用户
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserNotFound
		}
		return err
	}

	// 更新配置字段
	for key, value := range config {
		switch key {
		case "user":
			if v, ok := value.(string); ok {
				user.User = &v
			}
		case "pass":
			if v, ok := value.(string); ok {
				user.Pass = &v
			}
		case "key":
			if v, ok := value.(string); ok {
				user.Key = &v
			}
		case "appId":
			if v, ok := value.(string); ok {
				user.AppId = &v
			}
		case "notifyUrl":
			if v, ok := value.(string); ok {
				user.NotifyUrl = &v
			}
		case "returnUrl":
			if v, ok := value.(string); ok {
				user.ReturnUrl = &v
			}
		case "close":
			if v, ok := value.(int); ok {
				user.Close = &v
			}
		case "payQf":
			if v, ok := value.(int); ok {
				user.PayQf = &v
			}
		case "wxpay":
			if v, ok := value.(string); ok {
				user.Wxpay = &v
			}
		case "zfbpay":
			if v, ok := value.(string); ok {
				user.Zfbpay = &v
			}
		case "lastheart":
			if v, ok := value.(int64); ok {
				user.Lastheart = &v
			}
		case "lastpay":
			if v, ok := value.(int64); ok {
				user.Lastpay = &v
			}
		case "jkstate":
			if v, ok := value.(int); ok {
				user.Jkstate = &v
			}
		}
	}

	// 保存更新
	return s.userRepo.Update(user)
}
