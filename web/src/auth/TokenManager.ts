// Token management: access token stored in memory (never localStorage),
// refresh token in HttpOnly/Secure/SameSite=Strict cookie.
// Silent refresh loop and theft detection on reuse.
