# API Compatibility

LiteAIG public HTTP APIs expose a small compatibility contract through the `LiteAIG-API-Version` header.

## Request Header

Clients may omit `LiteAIG-API-Version` to use the current contract, or send an explicitly supported version.

Current version:

```text
LiteAIG-API-Version: 2026-09-09
```

Deprecated but still accepted version:

```text
LiteAIG-API-Version: 2026-09-01
```

Unsupported versions fail before reaching business logic:

```json
{"error":{"code":"UNSUPPORTED_API_VERSION","params":{"currentVersion":"2026-09-09"}}}
```

## Response Headers

Every Gateway and Admin API route handled by LiteAIG includes the current contract version:

```text
LiteAIG-API-Version: 2026-09-09
```

Deprecated requests are served with standard deprecation metadata:

```text
Deprecation: true
Sunset: Wed, 09 Dec 2026 00:00:00 GMT
```

## Policy

Within a supported API contract version, LiteAIG may add optional response fields, new enum values documented as extensible, and new routes. It must not remove required fields, change existing field meanings, change stable error codes, or narrow accepted request shapes without publishing a new version and deprecating the old one.

Deprecations require a supported overlap window and a `Sunset` header. Clients should pin a version for production integrations and monitor `Deprecation` headers during upgrades.
