## Datafeed Schema

```json
{
  "general": {
    "version": 0, // Major version of data feed
    "update_timestamp": "2024-10-07T18:04:52.803041Z", // When the datafeed was last updated
    "connected_clients": 0, // Total number of connected clients
    "unique_users": 0 // Total number of connected users unique by CID
  },
  "pilots": [ // List of online pilots
    "callsign": "N12345",
    "cid": 999999,
    "name": "John Doe", 
    "pilot_rating": 1,
    "latitude": 32.5,
    "longitude", -117.5,
    "altitude": 4502,
    "groundspeed": 92,
    "transponder": "2000",
    "heading": 92,                                // Degrees magnetic
    "last_updated": "2024-10-07T18:04:51.223342Z" // The time this pilot's information was last updated
  ],
  "ratings": [ // List of network ratings
    {
      "id": <network rating ID>,
      "short_name": "<network rating short-form name>",
      "long_name": "<network rating long-form name>"
    },
    ...
  ],
  "pilot_ratings": [ // List of pilot ratings
    {
      "id": <pilot rating ID>,
      "short_name": "<pilot rating short-form name>",
      "long_name": "<network rating long-form name>"
    },
    ...
  ]
}  
```