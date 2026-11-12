# Platform administrator roles

The `profile` scope includes the current FastCAS identity role in UserInfo as `role` (`admin` or `member`). SDK 0.1.3 exposes it through Go `Identity.Role`, Python `identity['role']`, and TypeScript `identity.role` after issuer, signature, nonce and subject validation. The role comes from the authenticated UserInfo response, including when an ID token contains an older role.

Platforms can promote a newly created or already linked account when this verified value is `admin`. Local administrator decisions remain under platform control. A local account must still pass its own enabled-status and account-link checks before a session is issued.
