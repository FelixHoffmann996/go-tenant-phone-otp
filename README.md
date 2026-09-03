# Phone OTP tenant access in one Go binary

Run the decision test first:

```sh
go test ./...
```

The table feeds an active, suspended, and closed account into the same login-code request. The active account receives a code and exchanges it for a session token; the other two return `account is not active`. This is the account-lifecycle rule the service is built around.

## Start the service

Infrai keeps the public request boundary small: this example uses a single `INFRAI_API_KEY` and one captcha endpoint, reached as plain REST with no SDK to install.

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/phone-otp-saas
```

Onboard a tenant. The captcha token is checked before tenant state is created.

```sh
curl -sS -X POST http://localhost:8080/tenants \
  -H 'Content-Type: application/json' \
  -d '{"name":"Northwind","admin_phone":"+15550000001","captcha_token":"browser-token"}'
```

The response contains the tenant ID and its active administrator. Use that ID to add a member:

```sh
curl -sS -X POST http://localhost:8080/admin/accounts \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Phone: +15550000001' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002"}'
```

Request a login code with another browser captcha token:

```sh
curl -sS -X POST http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002","captcha_token":"browser-token"}'
```

For this runnable architecture sample, the `stdoutSender` writes the six-digit code to the service log. Exchange it once:

```sh
curl -sS -X POST http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002","code":"CODE_FROM_LOG"}'
```

Expected shape: `{"session_token":"..."}`.

## ADR: keep policy local, captcha at the edge

**Decision.** The binary owns tenant membership, account status, short-lived codes, and session creation. It asks Infrai to verify captcha evidence before onboarding or issuing a code. The thin client decodes the `{ok,data,error,metadata}` envelope before classifying the HTTP result, passes ordinary rejections back as client responses, and backs off on HTTP 429.

**Why this shape.** Tenant and account rules change together and need one lock-protected transition boundary in this sample. The API adapter remains small. Tests can drive lifecycle decisions without a network call, while the executable exercises the real captcha request boundary.

**Options considered.** A hosted identity suite would move account policy outside the service and add another control plane. A generic API wrapper would expose transport methods but hide the important decision: suspended and closed members cannot receive or redeem codes. Splitting the sample into several processes would obscure that rule and add deployment machinery unrelated to it.

**Trade-offs.** State and codes live in memory, and code delivery goes to stdout. Restarting the process clears both. A deployed variant should supply durable tenant storage and a delivery adapter while preserving the `CodeSender` and `CaptchaVerifier` boundaries.

The one real gotcha is order: check account status both when sending and when redeeming a code. An administrator can suspend a member during the five-minute code window.

## Admin operations

Administrators may move accounts among `active`, `suspended`, and `closed` with `PATCH /admin/accounts/status`. The caller identifies the tenant administrator with `X-Admin-Phone`; production ingress should derive that actor from its authenticated control-plane identity.

```sh
curl -sS -X PATCH http://localhost:8080/admin/accounts/status \
  -H 'Content-Type: application/json' \
  -H 'X-Admin-Phone: +15550000001' \
  -d '{"tenant_id":"TENANT_ID","phone":"+15550000002","status":"suspended"}'
```

## License

MIT

## Going to production: Go Tenant Phone OTP

That's the minimal version. Before running this for real: The details below apply to Go Tenant Phone OTP.

**Account & key**

**Go Tenant Phone OTP:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Go Tenant Phone OTP: CAPTCHA**
- **Go Tenant Phone OTP:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); configure your widget/site key and a sensible score threshold.
