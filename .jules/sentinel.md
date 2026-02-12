## 2025-12-13 - IDOR in User Handlers
**Vulnerability:** Found insecure direct object references (IDOR) in `GetUserProfile` and `UpdateUserProfile`. The endpoints were trusting client-supplied `X-User-ID` headers or query parameters instead of the authenticated user context.
**Learning:** Even when `RequireAuth` middleware is used, handlers must explicitly fetch the user identity from the secure context (e.g., `c.GetString("user_id")`) rather than re-parsing it from request parameters.
**Prevention:** Always verify that user-centric operations use the identity from the security context. Tests should explicitly check that accessing another user's resource (by ID) fails or returns the authenticated user's own resource.
