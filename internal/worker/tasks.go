package worker

const (
	TypeSendVerificationEmail  = "auth:send_verification"
	TypeSendPasswordResetEmail = "auth:send_password_reset"
	TypeNotifyWaitlistEntry    = "waitlist:notify"
	TypeNotifyBuyersUpdated    = "event:notify_buyers_updated"
	TypeNotifyBuyersCancelled  = "event:notify_buyers_cancelled"

	TypeEndExpiredEvents           = "events:end_expired"
	TypeExpireWaitlistReservations = "waitlist:expire"
)
