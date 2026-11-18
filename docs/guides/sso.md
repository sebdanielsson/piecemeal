---
description: Sign in to piecemeal through Pocket ID, Authelia, Authentik, Keycloak or any OpenID Connect provider, with allowed and admin groups.
---

# Single sign-on

piecemeal signs people in through any OpenID Connect provider, such as Pocket ID, Authelia, Authentik or Keycloak. It uses the authorization code flow with PKCE and checks the ID token's signature, issuer, audience and nonce.

## Set it up

1. Register a client at the provider:
   - **Callback (redirect) URL**: `https://piecemeal.example.com/api/piecemeal/v1/auth/oidc/callback`
   - **Scopes**: `openid profile email groups`, or whatever makes your provider put the user's groups in the ID token.
2. Make a group for the people who may use piecemeal, and one for its admins.
3. Configure piecemeal:

```yaml
    environment:
      PIECEMEAL_PUBLIC_URL: https://piecemeal.example.com
      PIECEMEAL_OIDC_ISSUER: https://id.example.com
      PIECEMEAL_OIDC_CLIENT_ID: piecemeal
      PIECEMEAL_OIDC_CLIENT_SECRET: ${PIECEMEAL_OIDC_CLIENT_SECRET}
      PIECEMEAL_OIDC_NAME: Pocket ID
      PIECEMEAL_OIDC_ALLOWED_GROUPS: media
      PIECEMEAL_OIDC_ADMIN_GROUPS: media-admins
```

The sign-in page then shows a **Sign in with Pocket ID** button next to the password form.

## Groups

- Members of an allowed group can sign in. They're created as users on their first sign-in, named after their `preferred_username`, or the part of their email before the `@`.
- Members of an admin group are admins. An admin group doesn't let anyone in by itself, so put admins in an allowed group too.
- The groups come from the ID token's `groups` claim. Set `PIECEMEAL_OIDC_GROUPS_CLAIM` if your provider calls it something else.
- piecemeal refuses to start with OIDC and no allowed groups, because every user can add torrents and write anywhere under the allowed roots. When the provider only has people you trust, set `PIECEMEAL_OIDC_ALLOW_ANY=true` instead.

Admin rights follow the groups at each sign-in, so someone taken out of an admin group stays an admin until they next sign in. To remove someone at once, take them out of the groups at the provider, then remove them under **Users**, which ends their sessions and revokes their API keys.

## The local admin

The local admin from `PIECEMEAL_ADMIN_USERNAME` and `PIECEMEAL_ADMIN_PASSWORD_HASH` keeps working beside SSO, so you can still get in when the provider is down. Set `PIECEMEAL_LOCAL_LOGIN=false` to turn password sign-in off entirely; the \*arrs then need API keys, which SSO users can create too.

## What users and admins can do

| | User | Admin |
| --- | --- | --- |
| Add, start, stop, move, rename and remove torrents | yes | yes |
| Manage categories and tags | yes | yes |
| Change the speed limits | yes | yes |
| Read RSS feeds and refresh them | yes | yes |
| Create API keys for themselves | yes | yes |
| Change settings, RSS feeds and rules | | yes |
| See and manage everyone's keys and sessions, users, the audit log and the log | | yes |
| Download a backup | | yes |

Every user can add torrents into any allowed root and remove any torrent with its data, so only let in people you'd trust with your downloads.
