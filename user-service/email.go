package main

import (
	"fmt"
	"net/smtp"
)

type EmailService struct {
	smtpHost string
	smtpPort string
	smtpUser string
	smtpPass string
}

func (e *EmailService) SendVerificationEmail(to string, userID string) {
	auth := smtp.PlainAuth("", e.smtpUser, e.smtpPass, e.smtpHost)

	verificationLink := fmt.Sprintf("http://localhost:8080/verify?user_id=%s", userID)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: Verify Your Email\r\n"+
		"\r\n"+
		"Please verify your email by clicking the link: %s\r\n", to, verificationLink))

	addr := fmt.Sprintf("%s:%s", e.smtpHost, e.smtpPort)
	if err := smtp.SendMail(addr, auth, e.smtpUser, []string{to}, msg); err != nil {
		fmt.Printf("Failed to send email to %s: %v\n", to, err)
	}
}
