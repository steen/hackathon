### Fixed

- web/auth: Register form's 401 response now renders invite-rejected copy ("That invite code wasn't accepted. Please check it and try again.") instead of the misleading invalid-credentials copy ("That username and password don't match…") inherited from the shared form-auth classifier. Adds `REASON_INVITE_REJECTED` and a Register-specific `classifyRegisterAuthError` / `registerAuthMessage`; Login's 401/403 → `REASON_INVALID_CREDENTIALS` path is unchanged. Closes #556.
