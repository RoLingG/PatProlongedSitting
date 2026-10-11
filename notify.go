package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	// notifCategoryID 是通知按钮所属的类别标识
	notifCategoryID = "prolonged-sitting-alert"
	// actionStopRing 停止响铃并进入下一阶段
	actionStopRing = "STOP_RING"
	// actionSnooze 延后提醒
	actionSnooze = "SNOOZE"
	// snoozeLabel 是延后按钮的文案，与 app.go 的 snoozeMinutes 对应
	snoozeLabel = "延后 5 分钟"
)

var (
	notifOnce sync.Once

	notifActionMu sync.Mutex
	// notifAction 是通知被点击时的回调，参数为用户按下的动作标识。
	// 用包级变量是因为 OnNotificationResponse 只能注册一次，且回调内拿不到 Notifier 实例。
	notifAction func(action string)
)

// Notifier 负责发送系统原生通知
type Notifier struct {
	iconPath string
}

// NewNotifier 创建通知器，iconPath 可为空
// （原生 API 不支持自定义图标，Windows 下使用应用自身图标，此参数仅为兼容旧签名）
func NewNotifier(iconPath string) *Notifier {
	return &Notifier{iconPath: iconPath}
}

// Initialize 初始化通知系统并注册类别与回调，须在 App.startup 中调用一次。
func (n *Notifier) Initialize(ctx context.Context) {
	notifOnce.Do(func() {
		if err := runtime.InitializeNotifications(ctx); err != nil {
			log.Printf("通知系统初始化失败: %v", err)
			return
		}

		if !runtime.IsNotificationAvailable(ctx) {
			log.Printf("当前平台不支持通知，提醒将只依赖提示音")
			return
		}

		// macOS 需显式授权；Windows / Linux 无权限系统，调用是安全的
		if authorized, err := runtime.CheckNotificationAuthorization(ctx); err == nil && !authorized {
			if _, err := runtime.RequestNotificationAuthorization(ctx); err != nil {
				log.Printf("请求通知授权失败: %v", err)
			}
		}

		err := runtime.RegisterNotificationCategory(ctx, runtime.NotificationCategory{
			ID: notifCategoryID,
			Actions: []runtime.NotificationAction{
				{ID: actionStopRing, Title: "停止响铃"},
				{ID: actionSnooze, Title: snoozeLabel},
			},
		})
		if err != nil {
			log.Printf("注册通知类别失败: %v", err)
		}

		runtime.OnNotificationResponse(ctx, func(result runtime.NotificationResult) {
			if result.Error != nil {
				log.Printf("通知响应出错: %v", result.Error)
				return
			}
			notifActionMu.Lock()
			fn := notifAction
			notifActionMu.Unlock()
			if fn != nil {
				// 直接点击通知主体时 ActionIdentifier 为空，按"停止响铃"处理
				fn(result.Response.ActionIdentifier)
			}
		})
	})
}

// SetActionHandler 注册通知被点击时的回调。
// 直接点击通知主体时 action 为空字符串。
func (n *Notifier) SetActionHandler(fn func(action string)) {
	notifActionMu.Lock()
	notifAction = fn
	notifActionMu.Unlock()
}

// Notify 发送一条提醒通知，附带「停止响铃」与「延后」两个按钮。
func (n *Notifier) Notify(ctx context.Context, title, message string) {
	err := runtime.SendNotificationWithActions(ctx, runtime.NotificationOptions{
		ID:         newNotificationID(),
		Title:      title,
		Body:       message,
		CategoryID: notifCategoryID,
		Data:       map[string]interface{}{"kind": "phaseEnd"},
	})
	if err != nil {
		log.Printf("发送通知失败: %v", err)
	}
}

// newNotificationID 生成唯一 ID。
// 不能用固定串：同 ID 的通知会替换旧的，连续提醒就只看得见最后一条。
func newNotificationID() string {
	return fmt.Sprintf("sit-alert-%d", time.Now().UnixNano())
}
