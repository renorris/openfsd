# openfsd REST & frontend interface

## Overview

Part of the single `openfsd` binary (`cmd/openfsd -web`; default runs FSD + web). Shares `internal/db` with the FSD server; live connection state comes from the FSD service HTTP API (`FSD_HTTP_SERVICE_ADDRESS`, default `http://127.0.0.1:13618`).

JSON under `/api/v1` for **operator automation** and map polling. First-party UI is a progressive-enhancement MPA: form login sets a signed **HttpOnly session cookie**; `/api/v1` dual-accepts that cookie **or** a Bearer access token. Operators automating the same workflows as the UI should use **Bearer API tokens** (not browser session cookies).

**Blast radius:** Admin-minted API tokens currently carry an **Administrator-equivalent** network rating claim and full config/token power. Treat them as operator credentials with admin blast radius until fine-grained scopes exist—not as multi-tenant “third-party app” keys.

### First-party HTML pages (no-JS primary path)
| Page | Routes | Authz |
|------|--------|-------|
| Login | `GET/POST /login`, `POST /logout` | public / session |
| Dashboard | `GET /dashboard` | any session (OBS+); **server-rendered connection summary** (table/counts from FSD service). Leaflet map is PE only (`credentials: 'same-origin'`) |
| Account | `GET /account`, `POST /account/password`, `POST /account/delete` | any session; change password (current required) + soft-delete account (optional hard-delete via `ALLOW_PERMANENT_ACCOUNT_DELETE`, default false). CSRF; password step-up on delete |
| Users (directory) | `GET /usereditor[?q&rating&sort&dir&page&cid&new&flash]`, `POST /usereditor/create`, `POST /usereditor/update` | **Supervisor+**; create + name/password + ratings (network ceiling ≤ actor; full pilot scale). CSRF; URL-owned filters; `dir_*` on POST for PRG |
| Config editor | `GET/POST /configeditor`, `POST /configeditor/create-token`, `POST /configeditor/reset-secret` | Administrator; CSRF on mutations |
| Sweatbox | `GET /sweatbox`, form POSTs under `/sweatbox/*` | **Instructor1+**; CSRF on mutations; proxies FSD service HTTP |
| Airport editor | `GET /airport-editor`, `POST /airport-editor/download-apt`, `POST /airport-editor/download-air` | **Instructor1+**; CSRF on download; **echo-only** (no disk/DB persistence of `.apt`/`.air`). Validate API: `POST /api/v1/editor/validate-*` also I1+ |

JSON under `/api/v1` remains for external consumers and map polling. Session dual-accept mutations work with **cookie + CSRF only** (no `Authorization` header required).

### Airport editor validation

- **Live client:** JS `parseAPT` / `parseAIR` + soft cross-file warnings (dep ICAO, aircraft far from field) on the Validate tab.
- **Confirm with server (optional):** `POST /api/v1/editor/validate-apt` and `POST /api/v1/editor/validate-air` with JSON `{"text":"…"}` (**Instructor1+**, dual-accept Bearer | cookie; CSRF when cookie). Response is standard `APIV1Response` with `data.errors`, plus `icao` / `surface_count` or `aircraft_count`. Transient request body only — never written to disk/DB.
- **Handoff:** download `.apt`/`.air`, then load on `/sweatbox` (no automatic push from editor → live session).
- Design: `docs/design/apt-air-editor.md`. JS unit tests: `webjs/` + `bash scripts/check-webjs.sh`.

### JS budget / map exception
First-party openfsd modules stay small and vanilla (no jQuery). The **dashboard route** may load **Leaflet** (vendor) + `dashboard.js` as a documented exception to the 30–50 KB compressed first-party budget. Failure mode: map is absent; connection summary HTML still works.

The **airport editor** (`/airport-editor`) is a second complexity-gate exception for map geometry authoring (Leaflet + first-party modules). Open/edit/download are JS-primary (toolbar FileReader + Blob download; Raw tab for text). Map region is inert when JS is off. Optional `POST /airport-editor/download-*` echo handlers remain for tests/tools and never write APT/AIR to disk or DB.

---

## Authentication

### Browser (first-party UI)
- `POST /login` (form) → signed session cookie `openfsd_session` (HttpOnly, SameSite=Lax)
- Session claims: CID, network rating, display name, expiry (stateless JWT, `token_type=session`)
- TTL: **24h** default; **30 days** with “Remember me”
- `POST /logout` clears the session cookie
- Cookie-authenticated API mutations require a CSRF synchronizer token (`csrf_token` form field or `X-CSRF-Token` header matching the `openfsd_csrf` cookie)
- Suspended/inactive ratings cannot open a web session (same as FSD policy)
- **Session cookies are revalidated against the DB on every use** (HTML + dual-accept API): missing or inactive/suspended certificates clear cookies and are rejected; claims (network rating + names) are overlaid from the DB so demotions take effect immediately.
- **Bearer access tokens on dual-accept resource groups** (`/api/v1/user|config|fsdconn|sweatbox|editor/*`) are revalidated the same way (KD-18): demotion, suspension, and soft-delete take effect on the next request. Login/refresh/fsd-jwt remain credential-based and are outside this middleware.

### Cookie `Secure` flag (`COOKIE_SECURE`)
| Condition | Secure |
|-----------|--------|
| `COOKIE_SECURE=true` (or `1`/`yes`) | forced **true** |
| `COOKIE_SECURE=false` (or `0`/`no`) | forced **false** |
| unset + TLS listener | **true** |
| unset + `X-Forwarded-Proto: https` | **true** |
| unset + plain HTTP (local compose default) | **false** |

**Production / reverse proxy:** terminate TLS at the proxy, strip or overwrite client `X-Forwarded-Proto`, and set **`COOKIE_SECURE=true`**. Do not rely on client-supplied XFP alone — any direct client can send that header when the app is reachable without a trusted proxy hop.

### External API (Bearer)
Most endpoints accept a valid JWT access token:
```
Authorization: Bearer <access_token>
```
- **API tokens** can be created via `/api/v1/config/createtoken` (Administrator) with a custom expiry date (max **90 days**). See the **Server Configuration** menu in the frontend UI to generate one.
- `createtoken` responses include additive `recommended_api_version`, `api_version_min`, and `api_version_max` so clients can pin the microversion header.
- Bearer-authenticated clients do **not** need CSRF (CSRF applies only when the request is authenticated via the session cookie).
- Dual-accept: a **valid** Bearer token wins over a session cookie; a garbage Bearer header does **not** disable CSRF if the session cookie is what authenticates the request.
- Bearer actors are **revalidated against the DB** on every protected resource request (rating/names overlay; inactive/deleted → 401).
- **Operator automation:** prefer minted API tokens over `/auth/login` or `/auth/refresh`. Tokens are admin-equivalent until scopes exist—store as secrets; rotate on compromise via secret reset.

---

## API versioning

openfsd keeps a durable URL major **`/api/v1`**. Rare **breaking** changes on **Stable** enveloped routes introduce a date **microversion** selected with the request header:

```http
OpenFSD-API-Version: 2026-07-28
```

| Rule | Detail |
|------|--------|
| Canonical form | `YYYY-MM-DD` after normalize; also accept `1.YYYYMMDD` and `latest` (→ max) |
| Default when omitted | **`max_version` (current)**; response sets `OpenFSD-API-Version-Defaulted: true` |
| Production clients | **MUST** send `OpenFSD-API-Version` with a supported pin |
| Additive changes | Free (new fields/endpoints); clients must ignore unknown JSON keys |
| Breaking changes (Stable) | Bump microversion; keep old pins for the support window |
| Envelope body `version` | Always major `"v1"`; microversion is **header-only** |
| Response headers | `OpenFSD-API-Version` (effective), `OpenFSD-API-Min-Version`, `OpenFSD-API-Max-Version`, optional `OpenFSD-API-Version-Defaulted`, `Vary: OpenFSD-API-Version` |

**Discovery (public, version-agnostic — never 400 on a bad pin):**

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1` | Same discovery payload as `/versions` |
| GET | `/api/v1/versions` | `min_version`, `max_version`, `versions[]`, `openapi` |
| GET | `/api/v1/openapi.json` | OpenAPI 3 from embedded YAML |
| GET | `/api/v1/openapi.yaml` | Canonical embed source |

Canonical OpenAPI file: `internal/web/openapi/openapi.v1.yaml` (`//go:embed`). No mirrored copy under `docs/`.

**Outside microversion reject:** `/api/v1/data/*`, `/api/v1/fsd-jwt`, auth login/refresh, discovery, OpenAPI. Resource groups (`/user`, `/config`, `/fsdconn`, `/sweatbox`, `/editor`) reject unknown/invalid pins with **400** envelope.

**Stability tiers:** existing enveloped user/config/fsdconn/editor routes are **Stable** (goldens under `testdata/api_v1/<pin>/`). New expansion routes may ship **Provisional** (shape may change without a microversion bump while Provisional). Design: `docs/design/rest-api-versioning.md`.

Baseline pin (first supported): **`2026-07-28`**.

---

## Network Ratings
The API enforces role-based access control using `NetworkRating` values defined in `pkg/protocol`. Key thresholds:
- **Instructor1–3 (8–10)**: Sweatbox instructor UI + JSON proxies; airport editor + validate API. Cannot open the Users directory.
- **Supervisor (11)**: User editor (create, name, password, ratings); kick active connections. Network rating assignments capped at own rating; pilot ratings use the full official scale.
- **Administrator (12)**: Server configuration, JWT secret reset, API tokens.
- **Suspended (0) / Inactive (-1)**: Cannot log in to the web UI or obtain FSD JWTs; existing session cookies are rejected on revalidation.

---

## Error Handling
All API responses follow a standard `APIV1Response` structure:
```text
{
  "version": "v1",
  "err": string | null,
  "data": object | null
}
```
- **err**: Contains an error message if the request fails; otherwise, null.
- **data**: Contains the response data if relevant.

Common HTTP status codes:
- **200 OK**: Request succeeded.
- **201 Created**: Resource created successfully.
- **400 Bad Request**: Invalid request body or parameters.
- **401 Unauthorized**: Invalid or missing credentials.
- **403 Forbidden**: Insufficient permissions.
- **404 Not Found**: Resource not found.
- **500 Internal Server Error**: Server-side error.

---

## Endpoints

### Discovery & OpenAPI

#### GET /api/v1 and GET /api/v1/versions
Public discovery of supported microversions (same payload on both paths). **Version-agnostic:** a bad `OpenFSD-API-Version` does not cause 400.

**Response (200 OK)** `data`:
```json
{
  "major": "v1",
  "min_version": "2026-07-28",
  "max_version": "2026-07-28",
  "versions": ["2026-07-28"],
  "header": "OpenFSD-API-Version",
  "default": "max",
  "openapi": "/api/v1/openapi.json"
}
```

#### GET /api/v1/openapi.json and GET /api/v1/openapi.yaml
Public OpenAPI 3 document (embedded). Version-agnostic.

---

### Authentication

#### POST /api/v1/auth/login
Obtain access and refresh tokens using FSD login credentials.
NOTE: Do not use this endpoint for programmatic access from an external application.
Instead, generate an API token using the Configure Server menu via the frontend.

**Request Body**:
```json
{
  "cid": integer, // User certificate ID (min: 1)
  "password": string, // User password
  "remember_me": boolean // Extend refresh token validity to 30 days if true
}
```

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "access_token": string, // JWT access token
    "refresh_token": string // JWT refresh token
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid CID or password.
- **500 Internal Server Error**: Server-side error generating tokens.

**Permissions**: None (public endpoint).

---

#### POST /api/v1/auth/refresh
Refresh an access token using a refresh token.
NOTE: Do not use this endpoint for programmatic access from an external application.
Instead, generate an API token using the Configure Server menu via the frontend.

**Request Body**:
```json
{
  "refresh_token": string // JWT refresh token
}
```

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "access_token": string // New JWT access token
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid or non-refresh token.
- **500 Internal Server Error**: Server-side error retrieving JWT secret or generating token.

**Permissions**: None (public endpoint).

---

#### POST /api/v1/fsd-jwt
Obtain an FSD JWT token (see authentication-tokens in the FSD docs). This call matches the functionality of the VATSIM /api/fsd-jwt endpoint.

**Request Body** (form-encoded or JSON):
```json
{
  "cid": string, // User certificate ID
  "password": string // User password
}
```

**Response (200 OK)**:
```json
{
  "success": true,
  "token": string // FSD JWT token
}
```

**Errors**:
- **400 Bad Request**: Invalid CID format.
- **401 Unauthorized**: Invalid CID or password.
- **403 Forbidden**: User certificate is suspended or inactive.
- **500 Internal Server Error**: Server-side error retrieving JWT secret or generating token.

**Permissions**: None (public endpoint).

---

### User Management

#### POST /api/v1/user/load
Retrieve user information by CID.

**Request Body**:
```json
{
  "cid": integer // User certificate ID (min: 1)
}
```

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "cid": integer,
    "first_name": string,
    "last_name": string,
    "network_rating": integer // -1 to 12
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (non-self CID requires Supervisor rating).
- **404 Not Found**: User not found.
- **500 Internal Server Error**: Database error.

**Permissions**: Requires valid JWT access token. Users can retrieve their own info; Supervisor rating (11) required for other users' info.

---

#### PATCH /api/v1/user/update
Update user information by CID.
The CID itself is immutable and is only provided as reference of the user to update.

**Request Body**:
```json
{
  "cid": integer, // User certificate ID (min: 1)
  "password": string|null, // New password
  "first_name": string|null, // New first name
  "last_name": string|null, // New last name
  "network_rating": integer|null // New network rating (-1 to 12)
}
```

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "cid": integer,
    "first_name": string,
    "last_name": string,
    "network_rating": integer
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Supervisor rating required; cannot update users with higher rating).
- **404 Not Found**: User not found.
- **500 Internal Server Error**: Database error.

**Permissions**: Requires valid JWT access token and Supervisor rating (11). Cannot update users with a higher network rating.

---

#### POST /api/v1/user/create
Create a new user.

**Request Body**:
```json
{
  "password": string, // Password (min: 8 characters)
  "first_name": string|null, // First name
  "last_name": string|null, // Last name
  "network_rating": integer // Network rating (-1 to 12)
}
```

**Response (201 Created)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "cid": integer, // Assigned certificate ID
    "first_name": string|null,
    "last_name": string|null,
    "network_rating": integer
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Supervisor rating required; cannot create users with higher rating).
- **500 Internal Server Error**: Database error.

**Permissions**: Requires valid JWT access token and Supervisor rating (11). Created user's network rating cannot exceed the requester's.

---

### Configuration Management

#### GET /api/v1/config/load
Retrieve server configuration key-value pairs.

**Request**: No body required.

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "key_value_pairs": [
      {
        "key": string, // e.g., "welcome_message"
        "value": string
      }
    ]
  }
}
```

**Supported Keys**:
- `WELCOME_MESSAGE`
- `FSD_SERVER_HOSTNAME`
- `FSD_SERVER_IDENT`
- `FSD_SERVER_LOCATION`
- `API_SERVER_BASE_URL`

**Errors**:
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Administrator rating required).
- **500 Internal Server Error**: Database error.

**Permissions**: Requires valid JWT access token and Administrator rating (12).

---

#### POST /api/v1/config/update
Update server configuration key-value pairs.

**Request Body**:
```json
{
  "key_value_pairs": [
    {
      "key": string, // Configuration key
      "value": string // New value
    }
  ]
}
```

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": null
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Administrator rating required).
- **500 Internal Server Error**: Database error.

**Permissions**: Requires valid JWT access token and Administrator rating (12).

---

#### POST /api/v1/config/resetsecretkey
Reset the JWT secret key used for signing tokens.
Upon successfully calling this endpoint, this effectively invalidates *all* previously-administered authentication tokens. All users using the frontend will be logged out. All previously-generated API tokens will be invalidated.

**Request**: No body required.

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": null
}
```

**Errors**:
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Administrator rating required).
- **500 Internal Server Error**: Error generating or storing new secret key.

**Permissions**: Requires valid JWT access token and Administrator rating (12).

---

#### POST /api/v1/config/createtoken
Create a new API access token with a specified expiry (max 90 days).

**Blast radius:** minted token claims **Administrator** network rating. Treat as a full operator credential.

**Request Body**:
```json
{
  "expiry_date_time": string // ISO 8601 format (e.g., "2025-12-31T23:59:59.000Z")
}
```

**Response (201 Created)**:
```json
{
  "version": "v1",
  "err": null,
  "data": {
    "token": string, // JWT access token
    "recommended_api_version": string, // pin production clients should send
    "api_version_min": string,
    "api_version_max": string
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body, expiry in the past, or expiry more than 90 days out.
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Administrator rating required).
- **500 Internal Server Error**: Error generating or signing token.

**Permissions**: Requires valid JWT access token and Administrator rating (12).

---

### FSD Connection Management

#### POST /api/v1/fsdconn/kickuser
Kick an active user connection by callsign.

**Request Body**:
```json
{
  "callsign": string // User callsign
}
```

**Response (200 OK)**:
```json
{
  "version": "v1",
  "err": null,
  "data": null
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body.
- **401 Unauthorized**: Invalid bearer token.
- **403 Forbidden**: Insufficient permissions (Supervisor rating required).
- **404 Not Found**: Callsign not found.
- **500 Internal Server Error**: Error communicating with FSD HTTP service.

**Permissions**: Requires valid JWT access token and Supervisor rating (11).

---

### Data Endpoints

#### GET /api/v1/data/status.txt
Retrieve server status in plain text format. Mimics the VATSIM status.txt format.

**Request**: No body required.

**Response (200 OK)**:
- Content-Type: `text/plain`
- Body: Template-generated status text with carriage return newlines (`\r\n`).

**Errors**:
- **500 Internal Server Error**: Error retrieving base URL or generating template.

**Permissions**: None (public endpoint).

---

#### GET /api/v1/data/status.json
Retrieve server status in JSON format. Mimics the VATSIM status.json format.

**Response (200 OK)**:
```json
{
  "data": {
    "v3": [string], // URL to openfsd-data.json
    "servers": [string], // URL to openfsd-servers.json
    "servers_sweatbox": [string], // URL to sweatbox-servers.json
    "servers_all": [string] // URL to all-servers.json
  }
}
```

**Errors**:
- **500 Internal Server Error**: Error retrieving base URL or marshaling JSON.

**Permissions**: None (public endpoint).

---

#### GET /api/v1/data/openfsd-servers.txt
Retrieve server list in plain text format. Mimics the VATSIM vatsim-servers.txt format.

**Request**: No body required.

**Response (200 OK)**:
- Content-Type: `text/plain`
- Body: Template-generated server list with carriage return newlines (`\r\n`).

**Errors**:
- **500 Internal Server Error**: Error retrieving server info or generating template.

**Permissions**: None (public endpoint).

---

#### GET /api/v1/data/openfsd-servers.json
Retrieve server list in JSON format. Mimics the VATSIM vatsim-servers.json format.

**Response (200 OK)**:
```json
[
  {
    "ident": string,
    "hostname_or_ip": string,
    "location": string,
    "name": string,
    "clients_connection_allowed": integer,
    "client_connections_allowed": boolean,
    "is_sweatbox": boolean
  }
]
```

**Errors**:
- **500 Internal Server Error**: Error retrieving server info or marshaling JSON.

**Permissions**: None (public endpoint).

---

#### GET /api/v1/data/sweatbox-servers.json
Retrieve sweatbox server list in JSON format (same as openfsd-servers.json but with `is_sweatbox: true`).

**Response**: Same as `/openfsd-servers.json`.

**Errors**: Same as `/openfsd-servers.json`.

**Permissions**: None (public endpoint).

---

#### GET /api/v1/data/all-servers.json
Retrieve all servers in JSON format (same as openfsd-servers.json).

**Response**: Same as `/openfsd-servers.json`.

**Errors**: Same as `/openfsd-servers.json`.

**Permissions**: None (public endpoint).

---

#### GET /api/v1/data/openfsd-data.json
Retrieve cached datafeed of online pilots and ATC.

**Response (200 OK)**:
```json
{
  "pilots": [
    {
      // fsd.OnlineUserPilot fields
    }
  ],
  "atc": [
    {
      // fsd.OnlineUserATC fields
    }
  ]
}
```

**Errors**:
- **500 Internal Server Error**: Datafeed cache not available.

**Permissions**: None (public endpoint).

**Notes**: Data is cached and updated every 15 seconds via an internal worker.
