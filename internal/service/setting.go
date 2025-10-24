package service

import (
	"crypto/md5"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"
	"time"

	"vmqfox-api-go/internal/model"
	"vmqfox-api-go/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// 系统设置相关错误
var (
	ErrSettingNotFound = errors.New("setting not found")
	ErrInvalidSign     = errors.New("invalid signature")
)

// SettingService 系统设置服务接口
type SettingService interface {
	GetSystemConfig(userID uint) (*model.SystemConfigResponse, error)
	UpdateSystemConfig(userID uint, req *model.SystemConfigRequest) error
	GetSystemStatus(userID uint) (*model.SystemStatusResponse, error)
	GetGlobalSystemStatus() (*model.SystemStatusResponse, error)
	GetDashboard(userID uint) (*model.DashboardResponse, error)
	GetMonitorConfig(userID uint) (*model.MonitorConfigResponse, error)
	UpdateMonitorConfig(userID uint, req *model.MonitorConfigRequest) error
	ProcessMonitorHeart(req *model.MonitorHeartRequest) error
	ProcessMonitorPush(req *model.MonitorPushRequest) error
	GetSystemInfo() (*model.SystemInfoResponse, error)
	CheckUpdate(req *model.UpdateSystemRequest) (*model.UpdateSystemResponse, error)
	GetIPInfo() (*model.IPInfoResponse, error)
	// 监控端状态检查
	CheckAndUpdateMonitorStatus() error
}

// settingService 系统设置服务实现
type settingService struct {
	userRepo  repository.UserRepository
	orderRepo repository.OrderRepository
	startTime time.Time
}

// NewSettingService 创建系统设置服务
func NewSettingService(userRepo repository.UserRepository, orderRepo repository.OrderRepository) SettingService {
	return &settingService{
		userRepo:  userRepo,
		orderRepo: orderRepo,
		startTime: time.Now(),
	}
}

// GetSystemConfig 获取系统配置
func (s *settingService) GetSystemConfig(userID uint) (*model.SystemConfigResponse, error) {
	// 从users表获取用户配置
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, err
	}

	// 如果key为空，生成一个新的
	if user.Key == nil || *user.Key == "" {
		newKey := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%d", time.Now().Unix()))))
		user.Key = &newKey
		if err := s.userRepo.Update(user); err != nil {
			return nil, err
		}
	}

	// 转换为字符串
	lastheart := ""
	if user.Lastheart != nil {
		lastheart = strconv.FormatInt(*user.Lastheart, 10)
	}
	lastpay := ""
	if user.Lastpay != nil {
		lastpay = strconv.FormatInt(*user.Lastpay, 10)
	}
	jkstate := "0"
	if user.Jkstate != nil {
		jkstate = strconv.Itoa(*user.Jkstate)
	}
	closeStr := "5"
	if user.Close != nil {
		closeStr = strconv.Itoa(*user.Close)
	}
	payQfStr := "1"
	if user.PayQf != nil {
		payQfStr = strconv.Itoa(*user.PayQf)
	}

	return &model.SystemConfigResponse{
		User:      user.GetUser(),
		Pass:      user.GetPass(),
		NotifyUrl: user.GetNotifyUrl(),
		ReturnUrl: user.GetReturnUrl(),
		Key:       user.GetKey(),
		AppId:     user.GetAppId(),
		Lastheart: lastheart,
		Lastpay:   lastpay,
		Jkstate:   jkstate,
		Close:     closeStr,
		PayQf:     payQfStr,
		Wxpay:     user.GetWxpay(),
		Zfbpay:    user.GetZfbpay(),
		// 注册配置字段（TODO: 从global_settings表获取）
		RegisterEnabled:         "1",
		RegisterDefaultRole:     "admin",
		RegisterRequireApproval: "0",
		RegisterRateLimit:       "10",
	}, nil
}

// UpdateSystemConfig 更新系统配置
func (s *settingService) UpdateSystemConfig(userID uint, req *model.SystemConfigRequest) error {
	// 获取用户
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return err
	}

	// 处理密码加密
	if req.Pass != "" {
		// 检查是否已经是哈希值（bcrypt哈希值以$2a$、$2b$、$2x$、$2y$开头）
		if len(req.Pass) < 60 || (!strings.HasPrefix(req.Pass, "$2a$") &&
			!strings.HasPrefix(req.Pass, "$2b$") &&
			!strings.HasPrefix(req.Pass, "$2x$") &&
			!strings.HasPrefix(req.Pass, "$2y$")) {
			// 明文密码，需要加密
			hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Pass), bcrypt.DefaultCost)
			if err != nil {
				log.Printf("Failed to hash password: %v", err)
				return err
			}
			user.Pass = stringPtr(string(hashedBytes))
		} else {
			user.Pass = &req.Pass
		}
	}

	// 更新配置字段
	if req.User != "" {
		user.User = &req.User
	}
	if req.NotifyUrl != "" {
		user.NotifyUrl = &req.NotifyUrl
	}
	if req.ReturnUrl != "" {
		user.ReturnUrl = &req.ReturnUrl
	}
	if req.Close != "" {
		if closeInt, err := strconv.Atoi(req.Close); err == nil {
			user.Close = &closeInt
		}
	}
	if req.PayQf != "" {
		if payQfInt, err := strconv.Atoi(req.PayQf); err == nil {
			user.PayQf = &payQfInt
		}
	}
	if req.Wxpay != "" {
		user.Wxpay = &req.Wxpay
	}
	if req.Zfbpay != "" {
		user.Zfbpay = &req.Zfbpay
	}
	if req.AppId != "" {
		// 检查AppId是否已被其他用户使用
		existingUser, err := s.userRepo.GetByAppID(req.AppId)
		if err == nil && existingUser.Id != userID {
			return errors.New("商户ID已被使用")
		}
		user.AppId = &req.AppId
	}
	if req.Key != "" {
		user.Key = &req.Key
	}

	// 保存更新
	return s.userRepo.Update(user)
}

// stringPtr 辅助函数：字符串转指针
func stringPtr(s string) *string {
	return &s
}

// GetSystemStatus 获取系统状态
func (s *settingService) GetSystemStatus(userID uint) (*model.SystemStatusResponse, error) {
	// 获取今日统计
	today := time.Now().Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)

	todayStats, err := s.orderRepo.GetOrderStatsByDateRange(userID, today.Unix(), tomorrow.Unix())
	if err != nil {
		return nil, err
	}

	// 获取总统计
	totalStats, err := s.orderRepo.GetOrderStatsByDateRange(userID, 0, time.Now().Unix())
	if err != nil {
		return nil, err
	}

	// 获取用户配置
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, err
	}

	// 计算监控状态
	lastheartStr := ""
	if user.Lastheart != nil {
		lastheartStr = strconv.FormatInt(*user.Lastheart, 10)
	}
	lastpayStr := ""
	if user.Lastpay != nil {
		lastpayStr = strconv.FormatInt(*user.Lastpay, 10)
	}
	jkstateStr := "0"
	if user.Jkstate != nil {
		jkstateStr = strconv.Itoa(*user.Jkstate)
	}

	monitorStatus := s.calculateMonitorStatus(lastheartStr)
	lastHeartTime := s.formatTimestamp(lastheartStr)
	lastPayTime := s.formatTimestamp(lastpayStr)

	response := &model.SystemStatusResponse{
		TodayOrder:        todayStats.TotalOrders,
		TodaySuccessOrder: todayStats.SuccessOrders,
		TodayCloseOrder:   todayStats.ClosedOrders,
		TodayMoney:        todayStats.TotalAmount,
		CountOrder:        totalStats.TotalOrders,
		CountMoney:        totalStats.TotalAmount,
		Lastheart:         lastheartStr,
		Lastpay:           lastpayStr,
		Jkstate:           jkstateStr,
		MonitorStatus:     monitorStatus,
		LastHeartTime:     lastHeartTime,
		LastPayTime:       lastPayTime,
	}

	log.Printf("系统状态统计 - 用户ID: %d, 今日总订单: %d, 今日成功: %d, 今日失败: %d, 今日收入: %.2f",
		userID, response.TodayOrder, response.TodaySuccessOrder, response.TodayCloseOrder, response.TodayMoney)

	return response, nil
}

// GetGlobalSystemStatus 获取全局系统状态（所有用户的汇总数据）
// 只有超级管理员可以调用此接口
func (s *settingService) GetGlobalSystemStatus() (*model.SystemStatusResponse, error) {
	// 获取今日统计（所有用户）
	today := time.Now().Truncate(24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)

	todayStats, err := s.orderRepo.GetOrderStatsByDateRange(0, today.Unix(), tomorrow.Unix()) // userID=0表示所有用户
	if err != nil {
		return nil, err
	}

	// 获取总统计（所有用户）
	totalStats, err := s.orderRepo.GetOrderStatsByDateRange(0, 0, time.Now().Unix())
	if err != nil {
		return nil, err
	}

	// 全局监控状态返回空值，因为没有单一的全局监控端
	// 如果需要查看各个用户的监控状态，应该调用各自的GetSystemStatus
	response := &model.SystemStatusResponse{
		TodayOrder:        todayStats.TotalOrders,
		TodaySuccessOrder: todayStats.SuccessOrders,
		TodayCloseOrder:   todayStats.ClosedOrders,
		TodayMoney:        todayStats.TotalAmount,
		CountOrder:        totalStats.TotalOrders,
		CountMoney:        totalStats.TotalAmount,
		Lastheart:         "",
		Lastpay:           "",
		Jkstate:           "0",
		MonitorStatus:     0, // 0-未知
		LastHeartTime:     "",
		LastPayTime:       "",
	}

	log.Printf("全局系统状态统计 - 今日总订单: %d, 今日成功: %d, 今日失败: %d, 今日收入: %.2f",
		response.TodayOrder, response.TodaySuccessOrder, response.TodayCloseOrder, response.TodayMoney)

	return response, nil
}

// GetDashboard 获取仪表板数据
func (s *settingService) GetDashboard(userID uint) (*model.DashboardResponse, error) {
	// 获取系统状态
	status, err := s.GetSystemStatus(userID)
	if err != nil {
		return nil, err
	}

	// 获取系统信息
	sysInfo, err := s.GetSystemInfo()
	if err != nil {
		return nil, err
	}

	return &model.DashboardResponse{
		TodayOrder:        status.TodayOrder,
		TodaySuccessOrder: status.TodaySuccessOrder,
		TodayCloseOrder:   status.TodayCloseOrder,
		TodayMoney:        status.TodayMoney,
		CountOrder:        status.CountOrder,
		CountMoney:        status.CountMoney,
		PHPVersion:        "N/A (Go Version)",
		PHPOS:             runtime.GOOS,
		Server:            "Go/Gin Server",
		MySQL:             "MySQL 8.0+",
		Thinkphp:          "N/A (Go Version)",
		RunTime:           s.getUptime(),
		Ver:               sysInfo.AppVersion,
		GD:                "N/A (Go Version)",
	}, nil
}

// GetMonitorConfig 获取监控配置
func (s *settingService) GetMonitorConfig(userID uint) (*model.MonitorConfigResponse, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, err
	}

	lastheartStr := ""
	if user.Lastheart != nil {
		lastheartStr = strconv.FormatInt(*user.Lastheart, 10)
	}
	lastpayStr := ""
	if user.Lastpay != nil {
		lastpayStr = strconv.FormatInt(*user.Lastpay, 10)
	}
	jkstateStr := "0"
	if user.Jkstate != nil {
		jkstateStr = strconv.Itoa(*user.Jkstate)
	}

	return &model.MonitorConfigResponse{
		Jkstate:   jkstateStr,
		Lastheart: lastheartStr,
		Lastpay:   lastpayStr,
	}, nil
}

// UpdateMonitorConfig 更新监控配置
func (s *settingService) UpdateMonitorConfig(userID uint, req *model.MonitorConfigRequest) error {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return err
	}

	jkstate, err := strconv.Atoi(req.Jk)
	if err != nil {
		return err
	}
	user.Jkstate = &jkstate

	return s.userRepo.Update(user)
}

// ProcessMonitorHeart 处理监控心跳
func (s *settingService) ProcessMonitorHeart(req *model.MonitorHeartRequest) error {
	// 确定用户
	var user *model.User
	var err error

	if req.AppID != "" {
		log.Printf("心跳请求包含AppID: %s，尝试查找对应用户", req.AppID)
		user, err = s.userRepo.GetByAppID(req.AppID)
		if err != nil {
			log.Printf("AppID查找失败: %s, 错误: %v", req.AppID, err)
			return fmt.Errorf("invalid appid: %s", req.AppID)
		}
		log.Printf("AppID %s 对应用户ID: %d", req.AppID, user.Id)
	} else {
		log.Printf("心跳请求未包含AppID，使用默认用户ID: 1")
		user, err = s.userRepo.GetByID(1)
		if err != nil {
			return err
		}
	}

	// 验证签名 - 适配Android端格式：md5(timestamp + key)
	expectedSign := fmt.Sprintf("%x", md5.Sum([]byte(req.T+user.GetKey())))
	if req.Sign != expectedSign {
		return ErrInvalidSign
	}

	// 更新心跳时间和监控状态
	now := time.Now().Unix()
	jkstate := 1
	user.Lastheart = &now
	user.Jkstate = &jkstate

	return s.userRepo.Update(user)
}

// ProcessMonitorPush 处理监控推送
func (s *settingService) ProcessMonitorPush(req *model.MonitorPushRequest) error {
	// 确定用户
	var user *model.User
	var err error

	if req.AppID != "" {
		user, err = s.userRepo.GetByAppID(req.AppID)
		if err != nil {
			return fmt.Errorf("invalid appid: %s", req.AppID)
		}
	} else {
		user, err = s.userRepo.GetByID(1)
		if err != nil {
			return err
		}
	}

	// 验证签名 - 适配Android端格式：md5(type + price + timestamp + key)
	signStr := req.Type + req.Price + req.T + user.GetKey()
	expectedSign := fmt.Sprintf("%x", md5.Sum([]byte(signStr)))
	if req.Sign != expectedSign {
		return ErrInvalidSign
	}

	// 根据价格和类型查找对应的待支付订单
	price, err := strconv.ParseFloat(req.Price, 64)
	if err != nil {
		return fmt.Errorf("invalid price: %s", req.Price)
	}

	orderType, err := strconv.Atoi(req.Type)
	if err != nil {
		return fmt.Errorf("invalid type: %s", req.Type)
	}

	// 查找该用户最近创建的匹配订单
	order, err := s.orderRepo.GetRecentPendingOrderByPriceAndType(user.Id, price, orderType)
	if err != nil {
		log.Printf("未找到匹配的订单: 用户ID=%d, 价格=%f, 类型=%d, 错误=%v", user.Id, price, orderType, err)
		// 即使没找到订单，也更新lastpay时间
	} else {
		// 更新订单状态为已支付
		order.State = model.OrderStatusPaid
		order.Pay_date = time.Now().Unix()

		err = s.orderRepo.Update(order)
		if err != nil {
			log.Printf("更新订单状态失败: 订单ID=%s, 错误=%v", order.Order_id, err)
		} else {
			log.Printf("订单支付成功: 订单ID=%s, 用户ID=%d, 价格=%f", order.Order_id, user.Id, price)
		}
	}

	// 更新最后支付时间
	now := time.Now().Unix()
	user.Lastpay = &now
	return s.userRepo.Update(user)
}

// GetSystemInfo 获取系统信息
func (s *settingService) GetSystemInfo() (*model.SystemInfoResponse, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &model.SystemInfoResponse{
		GoVersion:    runtime.Version(),
		GOOS:         runtime.GOOS,
		Server:       "VMQFox Go API Server",
		MySQLVersion: "MySQL 8.0+",
		AppVersion:   "v2.0.0",
		RunTime:      s.getUptime(),
		StartTime:    s.startTime,
		MemoryUsage:  fmt.Sprintf("%.2f MB", float64(m.Alloc)/1024/1024),
		GoroutineNum: runtime.NumGoroutine(),
	}, nil
}

// CheckUpdate 检查更新
func (s *settingService) CheckUpdate(req *model.UpdateSystemRequest) (*model.UpdateSystemResponse, error) {
	// 模拟检查更新逻辑
	return &model.UpdateSystemResponse{
		HasUpdate:      false,
		CurrentVersion: "v2.0.0",
		LatestVersion:  "v2.0.0",
		UpdateUrl:      "",
		UpdateLog:      "当前已是最新版本",
	}, nil
}

// GetIPInfo 获取IP信息
func (s *settingService) GetIPInfo() (*model.IPInfoResponse, error) {
	// 模拟IP信息
	return &model.IPInfoResponse{
		IP:       "127.0.0.1",
		Country:  "中国",
		Region:   "本地",
		City:     "本地",
		ISP:      "本地网络",
		Location: "本地服务器",
	}, nil
}

// getUptime 获取运行时间
func (s *settingService) getUptime() string {
	uptime := time.Since(s.startTime)
	days := int(uptime.Hours()) / 24
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%d天%d小时%d分钟", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	} else {
		return fmt.Sprintf("%d分钟", minutes)
	}
}

// calculateMonitorStatus 计算监控状态
// 返回值：0-未知 1-正常 2-异常
func (s *settingService) calculateMonitorStatus(lastHeartStr string) int {
	if lastHeartStr == "" {
		return 0 // 未知状态
	}

	heartTime, err := strconv.ParseInt(lastHeartStr, 10, 64)
	if err != nil || heartTime <= 0 {
		return 0 // 未知状态
	}

	// 心跳超时时间：180秒（3分钟）
	const heartbeatTimeout = 180
	currentTime := time.Now().Unix()

	if currentTime-heartTime < heartbeatTimeout {
		return 1 // 正常
	} else {
		return 2 // 异常
	}
}

// formatTimestamp 格式化时间戳
func (s *settingService) formatTimestamp(timestampStr string) string {
	if timestampStr == "" {
		return ""
	}

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil || timestamp <= 0 {
		return ""
	}

	return time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
}

// CheckAndUpdateMonitorStatus 检查并更新所有用户的监控端状态
func (s *settingService) CheckAndUpdateMonitorStatus() error {
	// 心跳超时时间：180秒（3分钟）
	const heartbeatTimeout = 180
	currentTime := time.Now().Unix()

	// 获取所有用户（分页获取，避免一次性加载过多数据）
	page := 1
	limit := 100

	for {
		users, total, err := s.userRepo.GetUsers(page, limit, "")
		if err != nil {
			return err
		}

		for _, user := range users {
			if user.Lastheart == nil || *user.Lastheart == 0 {
				// 没有心跳记录，设置为掉线状态
				jkstate := 0
				user.Jkstate = &jkstate
				s.userRepo.Update(user)
				continue
			}

			// 检查心跳是否超时
			if currentTime-*user.Lastheart >= heartbeatTimeout {
				// 心跳超时，设置为掉线状态
				jkstate := 0
				user.Jkstate = &jkstate
				s.userRepo.Update(user)
			}
			// 如果心跳正常，不需要更新，因为心跳接口会自动设置为1
		}

		// 检查是否还有更多用户
		if int64(page*limit) >= total {
			break
		}
		page++
	}

	return nil
}
