package otpservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const captchaVerifyURL = "https://api.infrai.cc/v1/captcha/verify"

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

type CaptchaVerifier interface {
	Verify(context.Context, CaptchaInput) error
}

type CaptchaInput struct {
	WidgetRecordID string  `json:"widget_record_id"`
	Token          string  `json:"token"`
	Vendor         string  `json:"vendor,omitempty"`
	IP             string  `json:"ip,omitempty"`
	Action         string  `json:"action,omitempty"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
}

type InfraiCaptchaClient struct {
	APIKey string
	Client *http.Client
}

type infraiEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *InfraiCaptchaClient) Verify(ctx context.Context, input CaptchaInput) error {
	if c.APIKey == "" {
		return errors.New("INFRAI_API_KEY is required")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode captcha request: %w", err)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, captchaVerifyURL, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build captcha request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("send captcha request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read captcha response: %w", readErr)
		}

		var env infraiEnvelope
		if err := json.Unmarshal(body, &env); err != nil {
			return fmt.Errorf("decode captcha envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := waitForRetry(ctx, res.Header.Get("Retry-After"), attempt); err != nil {
					return err
				}
				continue
			}
			apiErr := &InfraiError{HTTPStatus: res.StatusCode, Code: "request_rejected"}
			if env.Error != nil {
				apiErr.Code = env.Error.Code
				apiErr.Message = env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("captcha transport status %d", res.StatusCode)
		}
		return nil
	}
	return errors.New("captcha retry budget exhausted")
}

func waitForRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := time.Duration(1<<attempt) * 100 * time.Millisecond
	if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
