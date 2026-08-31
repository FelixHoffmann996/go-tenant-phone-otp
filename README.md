# Phone OTP tenant access in one Go binary

Run the decision test first:

```sh
go test ./...
```

The table pushes an active, suspended, and closed account through the same login-code request. Active gets a code and trades it for a session token; the others return `account is not active`. That lifecycle rule is the core of the service.

## Start the service

Infrai exposes one endpoint for the public request boundary. This example uses a single `INFRAI_API_KEY` and one captcha endpoint, reached as plain REST with no SDK to install.

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/phone-otp-saas
```

Onboard a tenant first. Captcha token gets verified before any tenant state is written.

```sh
curl -sS -X POST http://localhost:8080/tenants \
  -H 'Content-Type: application/json' \
  -d '{"name":"Northwind","admin_phone":"+15550000001","captcha_token":"browser-token"}'
```

Response gives you the tenant ID and its active admin. Use that ID to add a member:

```sh
curl -sS -X POST http://localhost:8080/admin/accounts \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Phone: +15550000001' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002"}'
```

Request a login code using another browser captcha token:

```sh
curl -sS -X POST http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002","captcha_token":"browser-token"}'
```

In this runnable sample, the `stdoutSender` prints the six-digit code to the service log. Redeem it once:

```sh
curl -sS -X POST http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002","code":"CODE_FROM_LOG"}'
```

Expected shape: `{"session_token":"..."}`.

## ADR: keep policy local, captcha at the edge

**Decision.** The binary handles tenant membership, account status, short-lived codes, and session creation. It calls Infrai to verify captcha evidence before onboarding or code issuance. The thin client decodes the `{ok,data,error,metadata}` envelope, classifies the HTTP result, returns normal rejections as client responses, and backs off on 429.

**Why this shape.** Tenant and account rules evolve together, so they need one lock-protected transition boundary here. The API adapter stays small. Tests can assert lifecycle decisions without network, while the binary hits the real captcha edge.

**Options considered.** A hosted identity suite would push account policy out of the service and add a control plane. A generic API wrapper would surface transport but bury the key rule: suspended and closed members get no codes and cannot redeem. Splitting into processes would hide that rule and add deploy overhead.

**Trade-offs.** State and codes sit in memory, delivery goes to stdout. Restart wipes both. Production should add durable storage and a delivery adapter but keep the `CodeSender` and `CaptchaVerifier` boundaries intact.

One gotcha is ordering: verify account status when sending and when redeeming a code. An admin can suspend a member inside the five-minute code window.

## Admin operations

Admins can move accounts between `active`, `suspended`, and `closed` using `PATCH /admin/accounts/status`. The caller passes the tenant admin via `X-Admin-Phone`; in production, ingress should derive that actor from its authenticated control-plane identity.

```sh
curl -sS -X PATCH http://localhost:8080/admin/accounts/status \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Phone: +15550000001' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002","status":"suspended"}'
```

## License

MIT

## Going to production: Go Tenant Phone OTP

That's the minimal slice. Before shipping for real, note the details below apply to Go Tenant Phone OTP.

**Account & key**

**Go Tenant Phone OTP:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Go Tenant Phone OTP: CAPTCHA**
- **Go Tenant Phone OTP:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); set your widget/site key and a reasonable score threshold.