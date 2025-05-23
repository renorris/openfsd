# openfsd REST & frontend interface

## Overview
This API provides programmatic access to manage users, configurations, authentication, and FSD connections. All API endpoints are versioned under `/api/v1` and use JSON for request and response bodies unless otherwise specified. Authentication is primarily handled via JWT bearer tokens.

---

## Authentication
Most endpoints require a valid JWT access token included in the `Authorization` header as a Bearer token:
```
Authorization: Bearer <access_token>
```
- **API tokens** can be created via `/api/v1/config/createtoken` with a custom expiry date. See the **Server Configuration** menu in the frontend UI to generate one.

---

## Network Ratings
The API enforces role-based access control using `NetworkRating` values defined in the `fsd` package. Key thresholds:
- **Supervisor (11)**: Can manage users (create, update, retrieve) and kick active connections.
- **Administrator (12)**: Can manage server configuration, reset JWT secret keys, and create API tokens.

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
