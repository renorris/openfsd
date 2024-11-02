package servercontext

import (
	"context"
	"crypto/rand"
	"database/sql"
	"github.com/go-playground/validator/v10"
	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/renorris/openfsd/database"
	"github.com/renorris/openfsd/datafeed"
	"github.com/renorris/openfsd/postoffice"
	"github.com/renorris/openfsd/protocol"
	"github.com/sethvargo/go-envconfig"
	"io"
	"log"
	"os"
	"time"
)

const VersionIdentifier = "openfsd v0.1-alpha"

const inMemoryDatabaseAddress = "tcp(127.0.0.1:33060)"

// serverContextSingleton is the main server context singleton
var serverContextSingleton *ServerContext

func InitializeServerContextSingleton(ctx *ServerContext) {
	serverContextSingleton = ctx
}

// Config returns the server configuration
func Config() *ServerConfig {
	return &serverContextSingleton.config
}

// PostOffice returns the server post office singleton
func PostOffice() *postoffice.PostOffice {
	return serverContextSingleton.postOffice
}

// JWTKey returns the server JWT private key
func JWTKey() []byte {
	return serverContextSingleton.jwtKey
}

// DB returns the server database singleton
func DB() *sql.DB {
	return serverContextSingleton.db
}

func DataFeed() *datafeed.DataFeed {
	return serverContextSingleton.dataFeed
}

type ServerConfig struct {
	FSDListenAddress  string `env:"FSD_ADDR, default=0.0.0.0:6809"`                           // FSD network frontend/port
	HTTPListenAddress string `env:"HTTP_ADDR, default=0.0.0.0:8080"`                          // HTTP network frontend/port
	TLSEnabled        bool   `env:"TLS_ENABLED"`                                              // Whether to **flag** that TLS is enabled somewhere between openfsd and the client
	TLSCertFile       string `env:"TLS_CERT_FILE"`                                            // TLS certificate file path
	TLSKeyFile        string `env:"TLS_KEY_FILE"`                                             // TLS key file path
	DomainName        string `env:"DOMAIN_NAME, default=INCORRECT_DOMAIN_NAME_CONFIGURATION"` // Server domain name e.g. myserver.com
	HTTPDomainName    string `env:"HTTP_DOMAIN_NAME"`                                         // HTTP domain name e.g. web.myserver.com (overrides DOMAIN_NAME for HTTP services if set)
	FSDDomainName     string `env:"FSD_DOMAIN_NAME"`                                          // FSD domain name e.g. fsd.myserver.com (overrides DOMAIN_NAME for FSD services if set)
	MySQLUser         string `env:"MYSQL_USER"`                                               // MySQL username
	MySQLPass         string `env:"MYSQL_PASS"`                                               // MySQL password
	MySQLNet          string `env:"MYSQL_NET"`                                                // MySQL network protocol e.g. tcp, unix, etc
	MySQLAddr         string `env:"MYSQL_ADDR"`                                               // MySQL network address e.g. 127.0.0.1:3306
	MySQLDBName       string `env:"MYSQL_DBNAME"`                                             // MySQL database name
	InMemoryDB        bool   `env:"IN_MEMORY_DB, default=false"`                              // Whether to use an ephemeral in-memory DB instead of a real MySQL server
	MOTD              string `env:"MOTD, default=openfsd"`                                    // Server "Message of the Day"
}

type ServerContext struct {
	config     ServerConfig
	postOffice *postoffice.PostOffice
	jwtKey     []byte
	db         *sql.DB
	dataFeed   *datafeed.DataFeed
}

const privateKeyFile = "./jwtprivatekey"

// New creates a new ServerContext.
// Panics on failure.
func New() *ServerContext {
	server := ServerContext{}

	// PostOffice is ready zero-initialized.
	server.postOffice = postoffice.NewPostOffice()

	// Parse config environment variables
	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second)
	if err := envconfig.Process(ctx, &server.config); err != nil {
		log.Fatal(err)
	}
	cancelCtx()

	if server.config.InMemoryDB {
		server.config.MySQLAddr = "127.0.0.1:33060"
		server.config.MySQLNet = "tcp"
		server.config.MySQLDBName = "openfsd"

		server.config.MySQLUser = ""
		server.config.MySQLPass = ""
	}

	// Ensure TLSEnabled is set if TLS for the internal HTTP server is enabled
	if server.config.TLSCertFile != "" {
		server.config.TLSEnabled = true
	}

	// Load the JWT private key
	server.jwtKey = loadOrCreateJWTKey(privateKeyFile)

	// Instantiate protocol validator
	protocol.V = validator.New(validator.WithRequiredStructEnabled())

	// Create SQL config
	cfg := mysql.NewConfig()
	cfg.User = server.config.MySQLUser
	cfg.Passwd = server.config.MySQLPass
	cfg.Net = server.config.MySQLNet
	cfg.Addr = server.config.MySQLAddr
	cfg.DBName = server.config.MySQLDBName
	cfg.Params = map[string]string{"parseTime": "true"}

	// Create SQL db
	var db *sql.DB
	var err error
	if db, err = sql.Open("mysql", cfg.FormatDSN()); err != nil {
		log.Fatal(err)
	}
	server.db = db

	if server.config.InMemoryDB {
		server.db.SetMaxOpenConns(1)
	} else {
		if err = database.Initialize(server.db); err != nil {
			log.Fatal(err)
		}
	}

	// Initialize data feed
	server.dataFeed = &datafeed.DataFeed{}

	return &server
}

// loadOrCreateJWTKey loads or creates the JWT key contained in the file `filePath`.
// Panics on error.
func loadOrCreateJWTKey(filePath string) (key []byte) {
	// Load JWT key file
	var jwtKeyFile *os.File
	var err error
	if jwtKeyFile, err = os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0600); err != nil {
		log.Fatal(err)
	}

	// Check if the file is empty
	var ret int64
	if ret, err = jwtKeyFile.Seek(0, io.SeekEnd); ret != 64 {
		// Seeked to end of file and the length wasn't equal to the expected key length: 64 bytes.
		// Assume the keyfile is empty and needs to be written.

		// Truncate
		if err = jwtKeyFile.Truncate(0); err != nil {
			log.Fatal(err)
		}

		// Seek to start
		if _, err = jwtKeyFile.Seek(0, io.SeekStart); err != nil {
			log.Fatal(err)
		}

		// Copy 64 random bytes into the file
		if _, err = io.CopyN(jwtKeyFile, rand.Reader, 64); err != nil {
			log.Fatal(err)
		}
	}

	// Seek back to the beginning
	if _, err = jwtKeyFile.Seek(0, io.SeekStart); err != nil {
		log.Fatal(err)
	}

	// Read the entirety of the jwt key file
	if key, err = io.ReadAll(jwtKeyFile); err != nil {
		log.Fatal(err)
	}

	// Close it
	if err = jwtKeyFile.Close(); err != nil {
		log.Fatal(err)
	}

	return
}
