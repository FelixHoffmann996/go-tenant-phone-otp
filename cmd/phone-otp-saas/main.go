package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	otp "example.com/phone-otp-saas"
)

type stdoutSender struct{}

func (stdoutSender) Send(tenantID, phone, code string) error {
	log.Printf("otp tenant=%s phone=%s code=%s", tenantID, phone, code)
	return nil
}

type server struct{ service *otp.Service }

type onboardingRequest struct {
	Name                  string `json:"name"`
	AdminPhone            string `json:"admin_phone"`
	CaptchaWidgetRecordID string `json:"captcha_widget_record_id"`
	CaptchaToken          string `json:"captcha_token"`
}

type accountRequest struct {
	TenantID string            `json:"tenant_id"`
	Phone    string            `json:"phone"`
	Status   otp.AccountStatus `json:"status,omitempty"`
}

type codeRequest struct {
	TenantID              string `json:"tenant_id"`
	Phone                 string `json:"phone"`
	Code                  string `json:"code,omitempty"`
	CaptchaWidgetRecordID string `json:"captcha_widget_record_id,omitempty"`
	CaptchaToken          string `json:"captcha_token,omitempty"`
}

func main() {
	client := &otp.InfraiCaptchaClient{APIKey: os.Getenv("INFRAI_API_KEY")}
	app := &server{service: otp.NewService(client, stdoutSender{})}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tenants", app.onboard)
	mux.HandleFunc("POST /admin/accounts", app.addAccount)
	mux.HandleFunc("PATCH /admin/accounts/status", app.setStatus)
	mux.HandleFunc("POST /login/code", app.sendCode)
	mux.HandleFunc("POST /login/verify", app.verifyCode)

	address := ":8080"
	log.Printf("phone OTP service listening on %s", address)
	log.Fatal(http.ListenAndServe(address, mux))
}

func (s *server) onboard(w http.ResponseWriter, r *http.Request) {
	var input onboardingRequest
	if !decode(w, r, &input) {
		return
	}
	tenant, err := s.service.OnboardTenant(r.Context(), input.Name, input.AdminPhone, captcha(input.CaptchaWidgetRecordID, input.CaptchaToken, r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tenant)
}

func (s *server) addAccount(w http.ResponseWriter, r *http.Request) {
	var input accountRequest
	if !decode(w, r, &input) {
		return
	}
	account, err := s.service.AddAccount(input.TenantID, r.Header.Get("X-Admin-Phone"), input.Phone)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *server) setStatus(w http.ResponseWriter, r *http.Request) {
	var input accountRequest
	if !decode(w, r, &input) {
		return
	}
	if err := s.service.SetAccountStatus(input.TenantID, r.Header.Get("X-Admin-Phone"), input.Phone, input.Status); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) sendCode(w http.ResponseWriter, r *http.Request) {
	var input codeRequest
	if !decode(w, r, &input) {
		return
	}
	if err := s.service.SendLoginCode(r.Context(), input.TenantID, input.Phone, captcha(input.CaptchaWidgetRecordID, input.CaptchaToken, r)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "code_sent"})
}

func (s *server) verifyCode(w http.ResponseWriter, r *http.Request) {
	var input codeRequest
	if !decode(w, r, &input) {
		return
	}
	token, err := s.service.VerifyLoginCode(input.TenantID, input.Phone, input.Code)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_token": token})
}

func captcha(widgetRecordID, token string, r *http.Request) otp.CaptchaInput {
	ip := strings.Split(r.RemoteAddr, ":")[0]
	return otp.CaptchaInput{WidgetRecordID: widgetRecordID, Token: token, IP: ip, Action: "phone_login", ScoreThreshold: 0.5}
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var apiErr *otp.InfraiError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		status = apiErr.HTTPStatus
	}
	if errors.Is(err, otp.ErrInvalidInput) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, otp.ErrAccountInactive) || errors.Is(err, otp.ErrCodeRejected) {
		status = http.StatusForbidden
	}
	if errors.Is(err, otp.ErrTenantNotFound) || errors.Is(err, otp.ErrAccountNotFound) {
		status = http.StatusNotFound
	}
	if errors.Is(err, otp.ErrAdminRequired) {
		status = http.StatusForbidden
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v at %s", err, time.Now().Format(time.RFC3339))
	}
}
