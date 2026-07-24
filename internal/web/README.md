# openfsd REST & frontend interface

## Overview

Part of the single `openfsd` binary (`cmd/openfsd -web`; default runs FSD + web). Shares `internal/db` with the FSD server; live connection state comes from the FSD service HTTP API (`FSD_HTTP_SERVICE_ADDRESS`, default `http://127.0.0.1:13618`).

JSON under `/api/v1` for external tools and map polling. First-party UI is a progressive-enhancement MPA: form login sets a signed **HttpOnly session cookie**; `/api/v1` dual-accepts that cookie **or** a Bearer access token. External tools should use Bearer API tokens.

### First-party HTML pages (no-JS primary path)
| Page | Routes | Authz |
|------|--------|-------|
| Login | `GET/POST /login`, `POST /logout` | public / session |
| Dashboard | `GET /dashboard` | session; **server-rendered connection summary** (table/counts from FSD service). Leaflet map is PE only (`credentials: 'same-origin'`) |
| Users (directory) | `GET /usereditor[?q&rating&sort&dir&page&cid&new&flash]`, `POST /usereditor/create`, `POST /usereditor/update` | Instructor1+: directory + rating adjust; Supervisor+: create + name/password. CSRF; URL-owned filters; `dir_*` on POST for PRG |
| Config editor | `GET/POST /configeditor`, `POST /configeditor/create-token`, `POST /configeditor/reset-secret` | Administrator; CSRF on mutations |
| Sweatbox | `GET /sweatbox`, form POSTs under `/sweatbox/*` | Administrator; CSRF on mutations; proxies FSD service HTTP |
| Airport editor | `GET /airport-editor`, `POST /airport-editor/download-apt`, `POST /airport-editor/download-air` | Administrator; CSRF on download; **echo-only** (no disk/DB persistence of `.apt`/`.air`) |

JSON under `/api/v1` remains for external consumers and map polling. Admin mutations work with **cookie + CSRF only** (no `Authorization` header required).

### Airport editor validation

- **Live client:** JS `parseAPT` / `parseAIR` + soft cross-file warnings (dep ICAO, aircraft far from field) on the Validate tab.
- **Confirm with server (optional):** `POST /api/v1/editor/validate-apt` and `POST /api/v1/editor/validate-air` with JSON `{"text":"…"}` (Admin, dual-accept Bearer | cookie; CSRF when cookie). Response is standard `APIV1Response` with `data.errors`, plus `icao` / `surface_count` or `aircraft_count`. Transient request body only — never written to disk/DB.
- **Handoff:** download `.apt`/`.air`, then load on `/sweatbox` (no automatic push from editor → live session).
- Design: `docs/design/apt-air-editor.md`. JS unit tests: `webjs/` + `bash scripts/check-webjs.sh`.

### JS budget / map exception
First-party openfsd modules stay small and vanilla (no jQuery). The **dashboard route** may load **Leaflet** (vendor) + `dashboard.js` as a documented exception to the 30–50 KB compressed first-party budget. Failure mode: map is absent; connection summary HTML still works.

The **airport editor** (`/airport-editor`) is a second complexity-gate exception for map geometry authoring (Leaflet + first-party modules). Essential data path without JS: paste `apt_text` / `air_text` + CSRF form echo-download. Map region is inert when JS is off. Download handlers never write APT/AIR to disk or DB.

---

## Authentication

### Browser (first-party UI)
- `POST /login` (form) → signed session cookie `openfsd_session` (HttpOnly, SameSite=Lax)
- Session claims: CID, network rating, display name, expiry (stateless JWT, `token_type=session`)
- TTL: **24h** default; **30 days** with “Remember me”
- `POST /logout` clears the session cookie
- Cookie-authenticated API mutations require a CSRF synchronizer token (`csrf_token` form field or `X-CSRF-Token` header matching the `openfsd_csrf` cookie)
- Suspended/inactive ratings cannot open a web session (same as FSD policy)
- **Rating in the cookie is fixed until expiry.** Demotion/suspension does not revoke existing sessions until `exp` unless the JWT secret is rotated (Configure Server → Reset JWT secret). Prefer shorter TTL if faster revoke is required.

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
- **API tokens** can be created via `/api/v1/config/createtoken` with a custom expiry date. See the **Server Configuration** menu in the frontend UI to generate one.
- Bearer-authenticated clients do **not** need CSRF (CSRF applies only when the request is authenticated via the session cookie).
- Dual-accept: a **valid** Bearer token wins over a session cookie; a garbage Bearer header does **not** disable CSRF if the session cookie is what authenticates the request.

---

## Network Ratings
The API enforces role-based access control using `NetworkRating` values defined in `pkg/protocol`. Key thresholds:
- **Instructor1–3 (8–10)**: Can open the Users directory and adjust network/pilot ratings up to their own ceilings (any target). Cannot create users or change name/password.
- **Supervisor (11)**: Full user mutation (create, name, password) when target network rating ≤ own; rating adjust as above; kick active connections.
- **Administrator (12)**: Can manage server configuration, reset JWT secret keys, and create API tokens.
- **Suspended (0) / Inactive (-1)**: Cannot log in to the web UI or obtain FSD JWTs.

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
Create a new API access token with a specified expiry.

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
    "token": string // JWT access token
  }
}
```

**Errors**:
- **400 Bad Request**: Invalid JSON body or expiry date in the past.
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
