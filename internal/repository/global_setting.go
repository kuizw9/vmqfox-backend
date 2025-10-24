package repository

import (
	"vmqfox-api-go/internal/model"

	"gorm.io/gorm"
)

// GlobalSettingRepository 全局设置仓库接口
type GlobalSettingRepository interface {
	Get(key string) (*model.GlobalSetting, error)
	Set(key, value string) error
	GetAll() ([]model.GlobalSetting, error)
	GetRegisterConfig() (*model.RegisterConfig, error)
}

// globalSettingRepository 全局设置仓库实现
type globalSettingRepository struct {
	db *gorm.DB
}

// NewGlobalSettingRepository 创建全局设置仓库
func NewGlobalSettingRepository(db *gorm.DB) GlobalSettingRepository {
	return &globalSettingRepository{db: db}
}

// Get 获取全局设置
func (r *globalSettingRepository) Get(key string) (*model.GlobalSetting, error) {
	var setting model.GlobalSetting
	err := r.db.Where("key = ?", key).First(&setting).Error
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

// Set 设置全局设置
func (r *globalSettingRepository) Set(key, value string) error {
	setting := &model.GlobalSetting{
		Key:   key,
		Value: value,
	}
	return r.db.Save(setting).Error
}

// GetAll 获取所有全局设置
func (r *globalSettingRepository) GetAll() ([]model.GlobalSetting, error) {
	var settings []model.GlobalSetting
	err := r.db.Find(&settings).Error
	return settings, err
}

// GetRegisterConfig 获取注册配置
func (r *globalSettingRepository) GetRegisterConfig() (*model.RegisterConfig, error) {
	config := &model.RegisterConfig{
		Enabled:         true,
		DefaultRole:     "admin",
		RequireApproval: false,
		RateLimit:       10,
	}

	// 获取注册开关
	if setting, err := r.Get("register_enabled"); err == nil {
		config.Enabled = setting.Value == "1"
	}

	// 获取默认角色
	if setting, err := r.Get("register_default_role"); err == nil && setting.Value != "" {
		config.DefaultRole = setting.Value
	}

	// 获取是否需要审核
	if setting, err := r.Get("register_require_approval"); err == nil {
		config.RequireApproval = setting.Value == "1"
	}

	// 获取频率限制
	if setting, err := r.Get("register_rate_limit"); err == nil && setting.Value != "" {
		// 这里可以解析为int，暂时使用默认值
		config.RateLimit = 10
	}

	return config, nil
}

