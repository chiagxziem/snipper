package worker

const (
	TypeSendVerificationEmail  = "email:send_verification"
	TypeSendPasswordResetEmail = "email:send_password_reset"

	TypeNotifyWaitlistEntry        = "waitlist:notify"
	TypeEndExpiredEvents           = "events:end_expired"
	TypeExpireWaitlistReservations = "waitlist:expire"
)
