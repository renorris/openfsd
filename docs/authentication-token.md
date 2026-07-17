# Authentication Tokens

FSD authentication tokens adhere to the [JSON Web Token](https://en.wikipedia.org/wiki/JSON_Web_Token) (JWT) standard.
They are retrieved via HTTPS and subsequently transmitted in **plaintext** to the FSD server as part of the login process (`#AP` / `#AA` **Token** field).

Historically, Add Pilot (`#AP`) and Add ATC (`#AA`) packets used plaintext passwords in the Token field.
On modern VATSIM, clients using protocol revision **100** or **101** present a JWT instead.
openfsd accepts either a local JWT (private network) or a configured auth path depending on deployment — see the openfsd README / config.

### Endpoint (public VATSIM)

```text
POST https://auth.vatsim.net/api/fsd-jwt
```

##### Request body

```json
{
  "cid":      "123456",
  "password": "s3cr3t"
}
```

##### Response body (success)

```json
{
  "success": true,
  "token":   "<jwt token>"
}
```

##### Response body (error)

```json
{
  "success":   false,
  "error_msg": "<error message>"
}
```

<br>

### Token fields

*See [JWT Standard Fields](https://en.wikipedia.org/wiki/JSON_Web_Token#Standard_fields).*

Public VATSIM FSD JWTs have been observed in the following shape (example payload; values illustrative):

##### Header

```json
{
  "typ": "JWT",
  "alg": "HS256"
}
```

##### Payload example

```json
{
  "iat": 1735772371,
  "nbf": 1735772251,
  "exp": 1735772671,
  "iss": "https://auth.vatsim.net/api/fsd-jwt",
  "sub": "123456",
  "aud": "fsd-live",
  "jti": "rK7v1yEs1TExNDI1S",
  "controller_rating": 0,
  "pilot_rating":      0
}
```

| Claim | Meaning (empirical) | Notes |
|-------|---------------------|--------|
| `sub` | VATSIM CID | Subject |
| `aud` | Audience | Observed `fsd-live` on the public network; other audiences **unconfirmed** |
| `controller_rating` | ATC rating claim | Numeric; full mapping to wire [Network Ratings](enumerations.md#network-ratings) is **not fully documented here** |
| `pilot_rating` | Pilot rating claim | Numeric; treat wire meaning as **unconfirmed** without a current official table |

openfsd’s **private** JWT path uses its own claims (including a single network rating), which is **not** identical to the public VATSIM payload above.

##### Encoded example

```text
eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpYXQiOjE3MzU3NzIzNzEsIm5iZiI6MTczNTc3MjI1MSwiZXhwIjoxNzM1NzcyNjcxLCJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEyMzQ1NiIsImF1ZCI6ImZzZC1saXZlIiwianRpIjoicks3djF5RXMxVEV4TkRJMVMiLCJjb250cm9sbGVyX3JhdGluZyI6MCwicGlsb3RfcmF0aW5nIjowfQ.3aqOBIqhAP9RndXN1lao9OPsqMixX2Yndn89NpsvVjA
```

(Signature verification requires the issuer’s secret; example signature is illustrative.)
