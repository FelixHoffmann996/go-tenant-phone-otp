package otpservice

import (
	"context"
	"errors"
	"testing"
)

type acceptingCaptcha struct{}

func (acceptingCaptcha) Verify(context.Context, CaptchaInput) error { return nil }

type recordingSender struct{ code string }

func (s *recordingSender) Send(_, _, code string) error {
	s.code = code
	return nil
}

func TestAccountLifecycleControlsOTPLogin(t *testing.T) {
	tests := []struct {
		name    string
		status  AccountStatus
		wantErr error
	}{
		{name: "active account logs in", status: AccountActive},
		{name: "suspended account is rejected", status: AccountSuspended, wantErr: ErrAccountInactive},
		{name: "closed account is rejected", status: AccountClosed, wantErr: ErrAccountInactive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &recordingSender{}
			service := NewService(acceptingCaptcha{}, sender)
			tenant, err := service.OnboardTenant(context.Background(), "Northwind", "+15550000001", CaptchaInput{Token: "test-captcha"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.AddAccount(tenant.ID, "+15550000001", "+15550000002"); err != nil {
				t.Fatal(err)
			}
			if err := service.SetAccountStatus(tenant.ID, "+15550000001", "+15550000002", tt.status); err != nil {
				t.Fatal(err)
			}

			err = service.SendLoginCode(context.Background(), tenant.ID, "+15550000002", CaptchaInput{Token: "test-captcha"})
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("SendLoginCode() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.VerifyLoginCode(tenant.ID, "+15550000002", sender.code); err != nil {
				t.Fatalf("VerifyLoginCode() error = %v", err)
			}
		})
	}
}
