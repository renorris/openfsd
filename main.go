package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/go-playground/validator/v10"
	_ "github.com/mattn/go-sqlite3"
	"github.com/renorris/openfsd/protocol"
	"github.com/sethvargo/go-envconfig"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

type ServerConfig struct {
	FsdListenAddr  string `env:"FSD_ADDR, default=0.0.0.0:6809"`
	HttpListenAddr string `env:"HTTP_ADDR, default=0.0.0.0:9086"`
	HttpsEnabled   bool   `env:"HTTPS_ENABLED, default=false"`
	TLSCertFile    string `env:"TLS_CERT_FILE"`
	TLSKeyFile     string `env:"TLS_KEY_FILE"`
	DatabaseFile   string `env:"DATABASE_FILE, default=./db/fsd.db"`
	MOTD           string `env:"MOTD, default=openfsd"`
}

var SC *ServerConfig
var DB *sql.DB
var PO *PostOffice
var JWTKey []byte

const TableCreateStatement = `	
	CREATE TABLE IF NOT EXISTS users (
  		cid INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  		password CHAR(60) NOT NULL,
	    rating TINYINT NOT NULL,
	    real_name VARCHAR(64) NOT NULL,
	    creation_time DATETIME NOT NULL
	);
	`

func StartFSDServer(fsdCtx context.Context) {
	// Attempt to resolve the previously-configured listen address
	addr, err := net.ResolveTCPAddr("tcp4", SC.FsdListenAddr)
	if err != nil {
		log.Fatal("Error resolving address: " + err.Error())
	}

	// Attempt to open a listener on that address
	listener, err := net.ListenTCP("tcp4", addr)
	if err != nil {
		log.Fatal("Error listening: " + err.Error())
	}

	// Defer listener close
	// This will force the listener goroutine to encounter an error and promptly close itself
	defer func() {
		closeErr := listener.Close()
		if closeErr != nil {
			log.Println("Error closing listener: " + closeErr.Error())
		}
	}()

	// Listen on a separate goroutine
	// connChan shall be closed when the listener closes (due to an error or surrounding context closed)
	connChan := make(chan *net.TCPConn)
	go func(connChan chan<- *net.TCPConn) {
		// Guarantee that connChan will be closed when we return
		defer close(connChan)

		for {
			// Wait for a connection on the listener
			conn, listenerErr := listener.AcceptTCP()
			if listenerErr != nil {
				return
			}

			// Wait for a consumer on connChan, OR return if the context cancels.
			select {
			case connChan <- conn:
			case <-fsdCtx.Done():
				return
			}
		}
	}(connChan)

	log.Println("FSD listening")

	// Poll from connChan, check if the channel is healthy
	// Also poll from our context, check if we need to return
	for {
		select {
		case conn, ok := <-connChan:
			if !ok {
				return
			}
			go HandleConnection(conn)
		case <-fsdCtx.Done():
			return
		}
	}
}

func configureProtocolValidator() {
	protocol.V = validator.New(validator.WithRequiredStructEnabled())
}

func configurePostOffice() {
	PO = &PostOffice{
		clientRegistry:     make(map[string]*FSDClient),
		supervisorRegistry: make(map[string]*FSDClient),
		geohashRegistry:    make(map[uint64][]*FSDClient),
	}
}

func configureJwt() {
	idBytes := make([]byte, 64)
	_, err := rand.Read(idBytes)
	if err != nil {
		log.Fatal(err)
	}
	JWTKey = idBytes
}

func configureDatabase() {
	db, err := sql.Open("sqlite3", SC.DatabaseFile)
	if err != nil {
		log.Panic(err)
	}

	_, err = db.Exec(TableCreateStatement)
	if err != nil {
		log.Panic(err)
	}

	// Check if the users table is empty
	// (is this the first time we've started up using this database?)
	usersEmpty, err := IsUsersTableEmpty(db)
	if err != nil {
		log.Panic(err)
	}

	// If it's empty, add a default admin user
	if usersEmpty {
		buf := make([]byte, 8) // since each byte is 2 hex characters
		if _, err = io.ReadFull(rand.Reader, buf); err != nil {
			log.Panic(err)
		}

		pwd := hex.EncodeToString(buf)

		cid := 100000
		pwdHash, err := bcrypt.GenerateFromPassword([]byte(pwd), 10)
		if err != nil {
			log.Panic(err)
		}
		rating := protocol.NetworkRatingADM
		realName := "Default Administrator"

		record, err := AddUserRecord(db, cid, string(pwdHash), rating, realName)
		if err != nil {
			log.Panic(err)
		}

		fmt.Printf("Added user: %s    CID: %d    Password: %s    Rating: Administrator\n", record.RealName, record.CID, pwd)
	}

	// Set global DB variable
	DB = db
}

func setupServerConfig() {
	ctx := context.Background()

	var c ServerConfig
	if err := envconfig.Process(ctx, &c); err != nil {
		log.Fatal(err)
	}

	SC = &c
}

func main() {
	setupServerConfig()
	configureProtocolValidator()

	configureDatabase()
	defer DB.Close()

	configureJwt()
	configurePostOffice()

	httpCtx, cancelHttp := context.WithCancel(context.Background())
	go StartHttpServer(httpCtx)
	defer cancelHttp()

	fsdCtx, cancelFsd := context.WithCancel(context.Background())
	go StartFSDServer(fsdCtx)
	defer cancelFsd()

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	// Wait for OS done signal
	<-done
}
