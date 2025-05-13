# Authentication Tokens

*See [JSON Web Token](https://en.wikipedia.org/wiki/JSON_Web_Token)*

FSD authentication tokens adhere to the JSON Web Token (JWT) standard. 
They are retrieved via HTTPS and subsequently transmitted in plaintext to the FSD server as part of the login process.

Add Pilot (`#AP`) and Add ATC (`#AA`) packets previously used plaintext passwords in the Token field. 
Now, *any client* using *any protocol revision* must use the new authentication token.

### Endpoint

```text
POST https://auth.vatsim.net/api/fsd-jwt
```

##### Request Body

```json
{
  "cid":      "123456",
  "password": "s3cr3t"
}
```

##### Response Body

```json
{
  "success": true,
  "token":   "<jwt token>"
}
```

##### Response Body (Error Cases)

```json
{
  "success":   false,
  "error_msg": "<error message>"
}
```

<br>

### Token Fields

*See [JWT Standard Fields](https://en.wikipedia.org/wiki/JSON_Web_Token#Standard_fields)*

VATSIM FSD JSON Web Tokens adhere to the following format:

##### Header

```json
{
  "typ": "JWT",
  "alg": "HS256"
}
```

##### Payload Example

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
Two custom `number` fields are used: `controller_rating` and `pilot_rating`.<br>
The Subject (`sub`) field specifies the user's VATSIM CID.

##### Encoded Example

```text
eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpYXQiOjE3MzU3NzIzNzEsIm5iZiI6MTczNTc3MjI1MSwiZXhwIjoxNzM1NzcyNjcxLCJpc3MiOiJodHRwczovL2F1dGgudmF0c2ltLm5ldC9hcGkvZnNkLWp3dCIsInN1YiI6IjEyMzQ1NiIsImF1ZCI6ImZzZC1saXZlIiwianRpIjoicks3djF5RXMxVEV4TkRJMVMiLCJjb250cm9sbGVyX3JhdGluZyI6MCwicGlsb3RfcmF0aW5nIjowfQ.3aqOBIqhAP9RndXN1lao9OPsqMixX2Yndn89NpsvVjA
```
