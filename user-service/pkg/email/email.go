package email

import (
	"fmt"
	"log"
	"net/smtp"
)

type EmailService struct {
	host string
	port string
	user string
	pass string
}

func NewEmailService(host, port, user, pass string) *EmailService {
	return &EmailService{
		host: host,
		port: port,
		user: user,
		pass: pass,
	}
}

func (e *EmailService) SendVerificationEmail(to, userID, token string) {
	if e.user == "" || e.pass == "" {
		log.Printf("Email not configured - would send verification email to %s", to)
		return
	}

	auth := smtp.PlainAuth("", e.user, e.pass, e.host)
	verificationLink := fmt.Sprintf("http://localhost:8080/auth/verify?user_id=%s&token=%s", userID, token)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: Verify Your Email - Music Streaming\r\n"+
		"Content-Type: text/html; charset=utf-8\r\n"+
		"\r\n"+
		"<h1>Welcome to Music Streaming!</h1>\r\n"+
		"<p>Please verify your email by clicking the link below:</p>\r\n"+
		"<a href=\"%s\">Verify Email</a>\r\n"+
		"<p>This link will expire in 24 hours.</p>\r\n", to, verificationLink))

	addr := fmt.Sprintf("%s:%s", e.host, e.port)
	if err := smtp.SendMail(addr, auth, e.user, []string{to}, msg); err != nil {
		log.Printf("Failed to send email to %s: %v", to, err)
	} else {
		log.Printf("Verification email sent to %s", to)
	}
}

func (e *EmailService) SendPasswordResetEmail(to, token string) {
	if e.user == "" || e.pass == "" {
		log.Printf("Email not configured - would send password reset email to %s", to)
		return
	}

	auth := smtp.PlainAuth("", e.user, e.pass, e.host)
	resetLink := fmt.Sprintf("http://localhost:3000/reset-password?token=%s", token)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: Reset Your Password - Music Streaming\r\n"+
		"Content-Type: text/html; charset=utf-8\r\n"+
		"\r\n"+
		"<h1>Password Reset Request</h1>\r\n"+
		"<p>Click the link below to reset your password:</p>\r\n"+
		"<a href=\"%s\">Reset Password</a>\r\n"+
		"<p>This link will expire in 1 hour.</p>\r\n"+
		"<p>If you didn't request this, please ignore this email.</p>\r\n", to, resetLink))

	addr := fmt.Sprintf("%s:%s", e.host, e.port)
	if err := smtp.SendMail(addr, auth, e.user, []string{to}, msg); err != nil {
		log.Printf("Failed to send password reset email to %s: %v", to, err)
	} else {
		log.Printf("Password reset email sent to %s", to)
	}
}
