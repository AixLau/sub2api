package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const billingRejectHoldContextKey = "billing_reject_hold_state"

type billingRejectHoldState struct {
	hold *service.BillingRejectHold
	// rejected 由计费拒绝点置位：本轮计费检查仍然不通过。
	rejected bool
}

func billingRejectHoldStateFrom(c *gin.Context) *billingRejectHoldState {
	value, ok := c.Get(billingRejectHoldContextKey)
	if !ok {
		return nil
	}
	state, _ := value.(*billingRejectHoldState)
	return state
}

// holdBillingReject 在余额不足的拒绝点调用。返回 true 表示本次不要写拒绝响应，
// 直接 return，由 withBillingRejectHold 挂起请求并稍后重跑计费检查。
func holdBillingReject(c *gin.Context, apiKeyService *service.APIKeyService, userID int64) bool {
	if state := billingRejectHoldStateFrom(c); state != nil {
		state.rejected = true
		return true
	}
	hold := apiKeyService.BeginBillingRejectHold(userID)
	if hold == nil {
		return false
	}
	c.Set(billingRejectHoldContextKey, &billingRejectHoldState{hold: hold, rejected: true})
	return true
}

// billingRejectHoldPassed 在计费检查通过后调用：清除该用户的拒绝计数，
// 唤醒其他挂起中的请求，并释放当前请求占用的挂起名额。
func billingRejectHoldPassed(c *gin.Context, apiKeyService *service.APIKeyService, userID int64) {
	apiKeyService.ClearBillingRejectHold(userID)
	if state := billingRejectHoldStateFrom(c); state != nil {
		state.hold.Done(service.BillingRejectHoldProbe)
	}
}

// withBillingRejectHold 包装鉴权中间件：当 auth 在余额不足拒绝点选择挂起时，
// 这里阻塞等待，并按轮询周期重跑 auth。一旦计费检查通过（充值、分配订阅等），
// auth 会像普通请求一样继续执行后续处理；挂起到期则用 reject 写出原本的拒绝响应。
func withBillingRejectHold(auth gin.HandlerFunc, reject func(*gin.Context)) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth(c)
		state := billingRejectHoldStateFrom(c)
		if state == nil {
			return
		}
		for {
			if c.IsAborted() || !state.rejected {
				// 计费已通过，或重跑时被其他检查拒绝（Key 被删、用户被禁用等）。
				state.hold.Done(service.BillingRejectHoldProbe)
				return
			}
			switch event := state.hold.Wait(c.Request.Context()); event {
			case service.BillingRejectHoldCanceled:
				state.hold.Done(event)
				c.AbortWithStatus(statusClientClosedRequest)
				return
			case service.BillingRejectHoldExpired:
				state.hold.Done(event)
				c.Header("Connection", "close")
				reject(c)
				return
			}
			state.rejected = false
			auth(c)
		}
	}
}
