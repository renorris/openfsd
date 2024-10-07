## openfsd REST Specification `/api/v1`

## User Management:

Object types:

### User Ratings:

| Rating        | Value |
|---------------|-------|
| Inactive      | -1    |
| Suspended     | 0     |
| Observer      | 1     |
| Student 1     | 2     |
| Student 2     | 3     |
| Student 3     | 4     |
| Controller 1  | 5     |
| Controller 2  | 6     |
| Controller 3  | 7     |
| Instructor 1  | 8     |
| Instructor 2  | 9     |
| Instructor 3  | 10    |
| Supervisor    | 11    |
| Administrator | 12    |

### Pilot Ratings:

| Rating                          | Value |
|---------------------------------|-------|
| Basic Member                    | 0     |
| Private Pilot License           | 1     |
| Instrument Rating               | 3     |
| Commercial Multi-Engine License | 7     |
| Airline Transport Pilot License | 15    |
| Flight Instructor               | 31    |
| Flight Examiner                 | 63    |

### User Record:

| Field            | Type    | Description                                      |
|------------------|---------|--------------------------------------------------|
| `cid`            | integer | Certificate ID                                   |
| `email`          | string  | Email                                            |
| `first_name`     | string  | First name                                       |
| `last_name`      | string  | Last name                                        |
| `password`       | string  | Primary account password                         |
| `fsd_password`   | string  | FSD password (a precaution as FSD is plaintext.) |
| `network_rating` | integer | Network (Controller) Rating                      |
| `pilot_rating`   | integer | Pilot Rating                                     |
| `updated_at`     | integer | Last modified time (epoch seconds)               |
| `created_at`     | integer | Creation time (epoch seconds)                    |

## Methods:

The client must send a valid token in the Authorization header when making requests:
```
Authorization: Bearer <token>
```

### Response value
All `/api/v1/users` calls returning status `200` provide an application/json response body:

```json
{
  "msg": <response status message>,
  "user": <user record of concern>
}
```

### Error Response Codes
Error codes for user API calls are as follows:

| Code  | Description             |
|-------|-------------------------|
| `400` | Bad request             |
| `401` | Invalid authorization   |
| `403` | Insufficient permission |
| `500` | Server error            |

### POST `/api/v1/users`

Create a user record

#### JSON Parameters:

| Field  | Type        | Description                                                          |
|--------|-------------|----------------------------------------------------------------------|
| `user` | User Record | User Record to create (`cid` omitted. It is automatically assigned.) |

#### Returns:
| Code  | Description              |
|-------|--------------------------|
| `200` | Success                  |

#### Example:
```
POST /api/v1/users
{
    user: {
        password: "12345",
        rating: 1,
        ... rest of params
    }
}
```

---

### GET `/api/v1/users/{cid}`

Fetch a user record

#### Path Request Parameter:
| Field | Type    | Description  |
|-------|---------|--------------|
| `cid` | integer | CID to query |

#### Returns:
| Code  | Description             |
|-------|-------------------------|
| `200` | Success                 |
| `404` | User not found          |

#### JSON Response Payload:
| Field  | Type        | Description          |
|--------|-------------|----------------------|
| `user` | User Record | returned user record |

Example:

```
GET /api/v1/users/100000

{
    user: {
        cid: 100000,
        rating: 12,
        created_at: "2024-09-22T23:21:05Z"
        ... rest of params
    }
}
```

___

### PUT `/api/v1/users`

Update a user record.

All fields *must* be set as per `PUT` convention, except for 
`password` and `fsd_password`, which are optional.

#### JSON Parameters:
| Field  | Type        | Description           |
|--------|-------------|-----------------------|
| `user` | User Record | User record to update |

#### Returns:
| Code  | Description             |
|-------|-------------------------|
| `201` | Success                 |
| `404` | CID not found           |

```
PUT /api/v1/users

{
    user: {
        cid: 100002, // CID to update
        password: "12345", // New password
        rating: 10, // Changed rating
        ... rest of params (ALL REQUIRED)
    }
}

{
    msg: "success",
    user: {
        ... updated user
    }
}
```

---

### DELETE  `/api/v1/users`

Delete a user record

#### JSON Request Parameters:
| Field  | Type        | Description         |
|--------|-------------|---------------------|
| `cid`  | integer     | CID to delete       |

#### Returns:
| Code  | Description             |
|-------|-------------------------|
| `204` | Success                 |
| `404` | User not found          |

Example:

```
DELETE /api/v1/users

Request payload:
{
    "cid": 100002
}

```