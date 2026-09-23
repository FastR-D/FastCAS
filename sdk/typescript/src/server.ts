import * as oidc from "openid-client";
import { createRemoteJWKSet, customFetch, jwtVerify, type JWTPayload } from "jose";
import { createHash, timingSafeEqual } from "node:crypto";

export class FastCASError extends Error {
  constructor(public readonly code: string, message: string, public readonly status = 400) { super(message); this.name = "FastCASError"; }
}
export interface Link { id: string; client_id: string; local_account_ref: string; subject: string; state: "prepared" | "active" | "revoked"; version: number; verified_at: string; }
export interface LinkIntent { id: string; client_id: string; local_account_ref: string; nonce: string; state: string; expires_at: string; }
export interface Identity { issuer: string; subject: string; name?: string; email?: string; emailVerified: boolean; sessionId?: string; }
export interface Transaction {
  state: string; nonce: string; verifier: string; bindingHash: string; expiresAt: number;
  purpose: "login" | "link" | "register"; returnTo: string; localAccountRef?: string; localSessionId?: string; intent?: LinkIntent;
}
/** The application must supply persistent storage. take() must atomically remove
 * the transaction so concurrent callbacks cannot both succeed. */
export interface TransactionStore {
  put(transaction: Transaction): Promise<void>;
  take(state: string): Promise<Transaction | undefined>;
}
export interface ReplayStore { /** Atomically insert if absent. */ accept(id: string, expiresAt: number): Promise<boolean>; }
export interface Configuration {
  issuer: string; clientId: string; clientSecret?: string; redirectUri: string;
  allowLoopbackHTTP?: boolean; timeoutSeconds?: number; fetch?: typeof fetch;
}
export interface BeginOptions { browserBinding: string; returnTo?: string; scopes?: string[]; }
export interface LinkOptions extends BeginOptions { localAccountRef: string; localSessionId: string; }
export interface RegistrationOptions extends BeginOptions { newLocalAccountRef: string; }
export interface FinishOptions { browserBinding: string; localAccountRef?: string; localSessionId?: string; }
export interface LoginResult { identity: Identity; transaction: Transaction; tokens: oidc.TokenEndpointResponse; }
export interface AccountLinkEvent { id: string; type: "account_link.revoked"; link: Link; }
export interface IdentityStatusEvent { id: string; type: "identity.status_changed"; subject: string; status: "active" | "disabled"; version: number; }
export type FastCASNotification = AccountLinkEvent | IdentityStatusEvent;

function digest(value: string): string { return createHash("sha256").update(value).digest("hex"); }
function basicComponent(value: string): string { return new URLSearchParams({ value }).toString().slice("value=".length); }
function equal(left: string, right: string): boolean { const a = Buffer.from(left); const b = Buffer.from(right); return a.length === b.length && timingSafeEqual(a, b); }
function safeReturnTo(value?: string): string {
  if (!value) return "/";
  if (!value.startsWith("/") || value.startsWith("//") || /[\\\r\n]/.test(value)) throw new FastCASError("invalid_return_to", "Use an application-relative return path");
  return value;
}
function checkURL(value: string, insecure: boolean): URL {
  const url = new URL(value);
  if (url.username || url.password || url.hash) throw new FastCASError("invalid_url", "Credentials and fragments are not allowed in endpoint URLs");
  if (url.protocol !== "https:" && !(insecure && url.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname))) throw new FastCASError("https_required", "HTTPS is required outside explicit loopback development");
  return url;
}

export class FastCAS {
  private discovery?: Promise<oidc.Configuration>;
  private jwks?: ReturnType<typeof createRemoteJWKSet>;
  private readonly request: typeof fetch;
  /** Call only from trusted operations after draining in-flight authentication.
   * Clears both event/resource JWKS and openid-client's ID-token configuration.
   * This does not revoke previously issued application sessions. */
  resetVerificationCache(): void {
    this.jwks = undefined;
    this.discovery = undefined;
  }
  constructor(readonly config: Configuration, private readonly transactions: TransactionStore) {
    const issuer = checkURL(config.issuer, !!config.allowLoopbackHTTP);
    if (issuer.origin !== config.issuer) throw new FastCASError("invalid_issuer", "Issuer must be an origin without a trailing slash");
    checkURL(config.redirectUri, !!config.allowLoopbackHTTP);
    if (!config.clientId) throw new FastCASError("client_required", "Client ID required");
    this.request = config.fetch ?? fetch;
  }
  private async configuration(): Promise<oidc.Configuration> {
    if (!this.discovery) {
      this.discovery = oidc.discovery(new URL(this.config.issuer), this.config.clientId,
        { redirect_uris: [this.config.redirectUri], id_token_signed_response_alg: "RS256" },
        this.config.clientSecret ? oidc.ClientSecretBasic(this.config.clientSecret) : oidc.None(),
        { timeout: this.config.timeoutSeconds ?? 10, [oidc.customFetch]: (url, init) => this.request(url, { ...init, body: init.body as BodyInit }),
          execute: this.config.allowLoopbackHTTP ? [oidc.allowInsecureRequests, oidc.enableNonRepudiationChecks] : [oidc.enableNonRepudiationChecks] })
        .then(configuration => {
          const metadata = configuration.serverMetadata();
          if (metadata.issuer !== this.config.issuer) throw new FastCASError("issuer_mismatch", "Unexpected discovery issuer");
          for (const value of [metadata.authorization_endpoint, metadata.token_endpoint, metadata.jwks_uri, metadata.userinfo_endpoint]) {
            if (!value) throw new FastCASError("discovery_incomplete", "Required OIDC endpoint missing");
            const endpoint = checkURL(value, !!this.config.allowLoopbackHTTP);
            if (endpoint.origin !== this.config.issuer) throw new FastCASError("endpoint_mismatch", "FastCAS endpoints must belong to the configured issuer");
          }
          return configuration;
        }).catch(error => { this.discovery = undefined; throw error; });
    }
    return this.discovery;
  }
  async beginLogin(options: BeginOptions): Promise<URL> { return this.begin("login", options); }
  async beginLink(options: LinkOptions): Promise<URL> {
    if (!options.localAccountRef || !options.localSessionId) throw new FastCASError("local_proof_required", "A recently authenticated local account and session are required");
    return this.begin("link", options);
  }
  /** Reserve a fresh, never-existing account ID under the application's signup
   * policy. This flow cannot be used to associate an existing local account. */
  async beginRegistration(options: RegistrationOptions): Promise<URL> {
    if (!options.newLocalAccountRef) throw new FastCASError("new_account_required", "A newly reserved local account ID is required");
    return this.begin("register", options);
  }
  private async begin(purpose: "login" | "link" | "register", options: BeginOptions | LinkOptions | RegistrationOptions): Promise<URL> {
    if (options.browserBinding.length < 32) throw new FastCASError("browser_binding_required", "Use a fresh secure browser-binding cookie");
    const configuration = await this.configuration();
    const transaction: Transaction = { state: oidc.randomState(), nonce: oidc.randomNonce(), verifier: oidc.randomPKCECodeVerifier(), bindingHash: digest(options.browserBinding), purpose, returnTo: safeReturnTo(options.returnTo), expiresAt: Date.now() + 300_000 };
    if (purpose === "link" || purpose === "register") {
      transaction.localAccountRef = purpose === "link" ? (options as LinkOptions).localAccountRef : (options as RegistrationOptions).newLocalAccountRef;
      if (purpose === "link") transaction.localSessionId = (options as LinkOptions).localSessionId;
      transaction.intent = await this.api<LinkIntent>("POST", "/api/v1/link-intents", { local_account_ref: transaction.localAccountRef }, { "Idempotency-Key": transaction.state });
      transaction.nonce = transaction.intent.nonce;
    }
    await this.transactions.put(transaction);
    return oidc.buildAuthorizationUrl(configuration, { response_type: "code", redirect_uri: this.config.redirectUri,
      scope: (options.scopes ?? ["openid", "profile", "email"]).join(" "), state: transaction.state, nonce: transaction.nonce,
      code_challenge_method: "S256", code_challenge: await oidc.calculatePKCECodeChallenge(transaction.verifier) });
  }
  async finishLogin(callback: URL, options: FinishOptions): Promise<LoginResult> {
    if (callback.origin + callback.pathname !== new URL(this.config.redirectUri).origin + new URL(this.config.redirectUri).pathname) throw new FastCASError("callback_mismatch", "Unexpected callback URL");
    const state = callback.searchParams.get("state");
    if (!state) throw new FastCASError("state_missing", "Callback state required");
    const transaction = await this.transactions.take(state);
    if (!transaction || transaction.expiresAt <= Date.now() || !equal(transaction.bindingHash, digest(options.browserBinding))) throw new FastCASError("transaction_invalid", "Login transaction expired, consumed, or belongs to another browser");
    if (transaction.purpose === "link" && (transaction.localAccountRef !== options.localAccountRef || transaction.localSessionId !== options.localSessionId)) throw new FastCASError("local_account_changed", "The local account or session changed during linking");
    const configuration = await this.configuration();
    const tokens = await oidc.authorizationCodeGrant(configuration, callback, { pkceCodeVerifier: transaction.verifier, expectedState: transaction.state, expectedNonce: transaction.nonce, idTokenExpected: true });
    const claims = tokens.claims();
    if (!claims?.sub || claims.iss !== this.config.issuer) throw new FastCASError("identity_invalid", "No verified subject returned");
    const profile = await oidc.fetchUserInfo(configuration, tokens.access_token, claims.sub);
    return { identity: { issuer: claims.iss, subject: claims.sub, name: profile.name, email: profile.email,
      emailVerified: profile.email_verified === true, sessionId: typeof claims.sid === "string" ? claims.sid : undefined }, transaction, tokens };
  }
  async prepareLink(result: LoginResult): Promise<Link> {
    const intent = result.transaction.intent;
    if (!["link", "register"].includes(result.transaction.purpose) || !intent) throw new FastCASError("link_required", "Only a completed linking or registration transaction may prepare a link");
    return this.api("POST", `/api/v1/link-intents/${encodeURIComponent(intent.id)}/prepare`, { nonce: intent.nonce, subject: result.identity.subject });
  }
  /** Call only after committing the project's pending link. Persist active state
   * after this returns. Retry this operation using the same link ID on failure. */
  async activateLink(id: string): Promise<Link> { return this.api("POST", `/api/v1/link-intents/${encodeURIComponent(id)}/activate`); }
  async resolveLink(subject: string): Promise<Link> { return this.api("GET", `/api/v1/account-links/resolve?subject=${encodeURIComponent(subject)}`); }
  async getLink(id: string): Promise<Link> { return this.api("GET", `/api/v1/account-links/${encodeURIComponent(id)}`); }
  async revokeLink(link: Link): Promise<Link> { return this.api("POST", `/api/v1/account-links/${encodeURIComponent(link.id)}/revoke`, undefined, { "If-Match": `"${link.version}"` }); }
  async refresh(refreshToken: string): Promise<oidc.TokenEndpointResponse> { return oidc.refreshTokenGrant(await this.configuration(), refreshToken); }
  async clientCredentials(scopes: string[]): Promise<oidc.TokenEndpointResponse> { return oidc.clientCredentialsGrant(await this.configuration(), { scope: scopes.join(" ") }); }
  async revokeToken(token: string): Promise<void> { return oidc.tokenRevocation(await this.configuration(), token); }
  async exchangeToken(subjectToken: string, resource: string, scopes: string[]): Promise<oidc.TokenEndpointResponse> {
    return oidc.genericGrantRequest(await this.configuration(), "urn:ietf:params:oauth:grant-type:token-exchange", { subject_token: subjectToken, subject_token_type: "urn:ietf:params:oauth:token-type:access_token", requested_token_type: "urn:ietf:params:oauth:token-type:access_token", resource, audience: resource, scope: scopes.join(" ") });
  }
  /** Resource servers use this to observe immediate revocation of delegated tokens. */
  async introspectToken(token: string): Promise<{ active: boolean; sub?: string; client_id?: string; aud?: string[]; scope?: string }> {
    if (!this.config.clientSecret) throw new FastCASError("confidential_client_required", "Introspection requires client authentication");
    const endpoint = (await this.configuration()).serverMetadata().introspection_endpoint;
    if (!endpoint || checkURL(endpoint, !!this.config.allowLoopbackHTTP).origin !== this.config.issuer) throw new FastCASError("endpoint_mismatch", "Invalid introspection endpoint");
    const response = await this.request(endpoint, { method: "POST", redirect: "manual", headers: { "Content-Type": "application/x-www-form-urlencoded", Authorization: "Basic " + Buffer.from(`${basicComponent(this.config.clientId)}:${basicComponent(this.config.clientSecret)}`).toString("base64") }, body: new URLSearchParams({ token }) });
    if (!response.ok || response.redirected) throw new FastCASError("introspection_failed", "Token introspection failed", response.status);
    const value = await response.json() as Record<string, unknown>;
    if (typeof value.active !== "boolean") throw new FastCASError("invalid_response", "Invalid introspection response");
    return value as { active: boolean; sub?: string; client_id?: string; aud?: string[]; scope?: string };
  }
  async deviceAuthorize(scopes: string[]) { return oidc.initiateDeviceAuthorization(await this.configuration(), { scope: scopes.join(" ") }); }
  async pollDevice(response: Awaited<ReturnType<FastCAS["deviceAuthorize"]>>, signal?: AbortSignal) { return oidc.pollDeviceAuthorizationGrant(await this.configuration(), response, undefined, { signal }); }
  private async verify(raw: string, audience: string, event = false, logout = false): Promise<JWTPayload> {
    const configuration = await this.configuration();
    this.jwks ??= createRemoteJWKSet(new URL(configuration.serverMetadata().jwks_uri!), { [customFetch]: this.request, timeoutDuration: 10_000, cooldownDuration: 30_000, cacheMaxAge: 300_000 });
    return (await jwtVerify(raw, this.jwks, { issuer: this.config.issuer, audience, algorithms: ["RS256"], clockTolerance: 60, ...(event ? { typ: "fastcas-event+jwt" } : logout ? { typ: "logout+jwt" } : {}), requiredClaims: event ? ["iss", "aud", "jti", "iat", "exp"] : ["iss", "aud", "sub", "iat", "exp"] })).payload;
  }
  /** Commit event.id deduplication, link.version comparison, and local updates in
   * one application database transaction before acknowledging the webhook.
   * A previously committed event is successful, not an authentication failure. */
  async handleEvent(raw: string, apply: (event: AccountLinkEvent) => Promise<void>): Promise<void> {
    return this.handleNotification(raw, async event => {
      if (event.type !== "account_link.revoked") throw new FastCASError("event_invalid", "Unsupported account-link event", 401);
      await apply(event);
    });
  }
  /** Apply in one local transaction. Identity status affects only FastCAS
   * sessions; independent project credentials remain under project policy. */
  async handleNotification(raw: string, apply: (event: FastCASNotification) => Promise<void>): Promise<void> {
    const claims = await this.verify(raw, this.config.clientId, true);
    const event = claims.event as { type?: unknown; link?: Partial<Link>; subject?: unknown; status?: unknown; version?: unknown } | undefined;
    const link = event?.link;
    if (!claims.jti || !claims.iat || Math.abs(Date.now() / 1000 - claims.iat) > 300) throw new FastCASError("event_invalid", "Invalid event claims", 401);
    if (event?.type === "identity.status_changed") {
      if (typeof event.subject !== "string" || !event.subject || (event.status !== "active" && event.status !== "disabled") ||
          !Number.isSafeInteger(event.version) || (event.version as number) < 2 || link !== undefined) throw new FastCASError("event_invalid", "Invalid identity status event", 401);
      await apply({ id: claims.jti, type: "identity.status_changed", subject: event.subject, status: event.status, version: event.version as number });
      return;
    }
    if (event?.type !== "account_link.revoked" || !link || link.client_id !== this.config.clientId ||
      typeof link.id !== "string" || !link.id || typeof link.subject !== "string" || !link.subject ||
      typeof link.local_account_ref !== "string" || !link.local_account_ref || link.state !== "revoked" ||
      !Number.isSafeInteger(link.version) || link.version! < 1) {
      throw new FastCASError("event_invalid", "Invalid account-link event", 401);
    }
    await apply({ id: claims.jti, type: "account_link.revoked", link: link as Link });
  }
  async verifyAccessToken(raw: string, audience: string, scopes: string[] = []): Promise<JWTPayload> {
    const claims = await this.verify(raw, audience);
    if (claims.token_use !== "access" || typeof claims.scope !== "string") throw new FastCASError("wrong_token_type", "An access token is required", 401);
    const granted = new Set(claims.scope.split(" "));
    if (scopes.some(scope => !granted.has(scope))) throw new FastCASError("insufficient_scope", "Required permission missing", 403);
    return claims;
  }
  async verifyLogout(raw: string): Promise<{ id: string; subject: string; sessionId?: string; expiresAt: number }> {
    const claims = await this.verify(raw, this.config.clientId, false, true);
    const events = claims.events as Record<string, unknown> | undefined;
    if (!events || Object.keys(events).length !== 1 || typeof events["http://schemas.openid.net/event/backchannel-logout"] !== "object" || claims.nonce !== undefined || !claims.jti || !claims.iat || Math.abs(Date.now() / 1000 - claims.iat) > 300) throw new FastCASError("logout_invalid", "Invalid back-channel logout token", 401);
    return { id: claims.jti, subject: claims.sub!, sessionId: typeof claims.sid === "string" ? claims.sid : undefined, expiresAt: (claims.exp ?? Date.now() / 1000 + 300) * 1000 };
  }
  async handleLogout(raw: string, replay: ReplayStore): Promise<{ subject: string; sessionId?: string }> {
    const result = await this.verifyLogout(raw);
    if (!await replay.accept(result.id, result.expiresAt)) throw new FastCASError("logout_replayed", "Logout token already processed", 401);
    return { subject: result.subject, sessionId: result.sessionId };
  }
  async diagnostics(): Promise<{ issuer: string; clientId: string; ready: boolean }> { await this.configuration(); return { issuer: this.config.issuer, clientId: this.config.clientId, ready: true }; }
  private async api<T>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
    if (!this.config.clientSecret) throw new FastCASError("confidential_client_required", "Account-link APIs require an application server credential");
    const auth = Buffer.from(`${encodeURIComponent(this.config.clientId)}:${encodeURIComponent(this.config.clientSecret)}`).toString("base64");
    const response = await this.request(this.config.issuer + path, { method, redirect: "error", signal: AbortSignal.timeout((this.config.timeoutSeconds ?? 10) * 1000), headers: { authorization: `Basic ${auth}`, accept: "application/json", ...(body === undefined ? {} : { "content-type": "application/json" }), ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
    if (!response.ok) {
      const error = await response.json().catch(() => ({})) as { code?: string; message?: string };
      throw new FastCASError(error.code ?? "request_failed", error.message ?? "FastCAS request failed", response.status);
    }
    return response.json() as Promise<T>;
  }
}
