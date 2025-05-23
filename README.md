# openfsd

**openfsd** is an open-source multiplayer flight simulation server implementing the modern VATSIM FSD protocol. It connects pilots and air traffic controllers in a shared virtual environment.

## Features

- Facilitate multiplayer flight simulation with VATSIM protocol compatibility.
- Integrate web-based management for users, settings, and connections.
- Support SQLite and PostgreSQL for persistent storage.

## Quick Start with Docker

The preferred way to run openfsd is using **Docker** and **Docker Compose**. See the [Deployment Wiki](https://github.com/renorris/openfsd/wiki/Deployment).

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### Steps

1. **Clone the Repository**:
   ```bash
   git clone https://github.com/renorris/openfsd.git
   cd openfsd
   ```

2. **Start with Docker Compose**:
   ```bash
   docker-compose up -d
   ```
   This launches the FSD server and web server sharing an SQLite database persisted in a named Docker volume. This setup will work great for most people running small servers.

3. **Configure the Server via Web Interface**:
    - Open `http://localhost:8000` in a browser.
    - Log in with the default administrator credentials (printed in the FSD server logs on first startup).
    - Navigate to the **Configure Server** menu
    - Set configuration values. See the [Configuration]() wiki.

4. **Connect**:
   See the [Client Connection Wiki](https://github.com/renorris/openfsd/wiki/Client-Connection) for client-specific instructions.

## API

The web server exposes APIs under `/api/v1` for authentication, user management, and configuration. Although a basic web interface is provided, users are encouraged to call this API from their own external applications. See the [API](https://github.com/renorris/openfsd/tree/main/web) documentation.
