package main

import (
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"time"
)

type FSDUserRecord struct {
	CID          int       `json:"cid"`
	Password     string    `json:"password,omitempty"`
	Rating       int       `json:"rating"`
	RealName     string    `json:"real_name"`
	CreationTime time.Time `json:"creation_time"`
}

// GetUserRecord returns a user record for a given CID
// If no user is found, this is not an error. FSDUserRecord will be nil, and error will be nil.
func GetUserRecord(db *sql.DB, cid int) (*FSDUserRecord, error) {
	row := db.QueryRow("SELECT * FROM users WHERE cid=? LIMIT 1", cid)

	var cidRecord int
	var pwd string
	var rating int
	var realName string
	var creationTime time.Time

	err := row.Scan(&cidRecord, &pwd, &rating, &realName, &creationTime)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &FSDUserRecord{
		CID:          cidRecord,
		Password:     pwd,
		Rating:       rating,
		RealName:     realName,
		CreationTime: creationTime,
	}, nil
}

func AddUserRecord(db *sql.DB, cid int, pwdHash string, rating int, realName string) (*FSDUserRecord, error) {
	t := time.Now()

	res, err := db.Exec("INSERT INTO users (cid, password, rating, real_name, creation_time) VALUES(?,?,?,?,?);", cid, pwdHash, rating, realName, t)
	if err != nil {
		return nil, err
	}

	resCID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	record := FSDUserRecord{
		CID:          int(resCID),
		Password:     "",
		Rating:       rating,
		RealName:     realName,
		CreationTime: t,
	}

	return &record, nil
}

func AddUserRecordSequential(db *sql.DB, pwdHash string, rating int, realName string) (*FSDUserRecord, error) {
	t := time.Now()

	res, err := db.Exec("INSERT INTO users (password, rating, real_name, creation_time) VALUES(?,?,?,?);", pwdHash, rating, realName, t)
	if err != nil {
		return nil, err
	}

	resCID, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	record := FSDUserRecord{
		CID:          int(resCID),
		Password:     "",
		Rating:       rating,
		RealName:     realName,
		CreationTime: t,
	}

	return &record, nil
}

// UpdateUserRecord updates a user record. It only updates the password field if it is non-empty.
func UpdateUserRecord(db *sql.DB, record *FSDUserRecord) error {
	var pwdHash []byte
	var err error
	if record.Password != "" {
		pwdHash, err = bcrypt.GenerateFromPassword([]byte(record.Password), 10)
		if err != nil {
			return err
		}
	}

	if pwdHash == nil {
		_, err := db.Exec("UPDATE users SET rating=?, real_name=? WHERE cid=?;", record.Rating, record.RealName, record.CID)
		if err != nil {
			return err
		}
	} else {
		_, err := db.Exec("UPDATE users SET password=?, rating=?, real_name=? WHERE cid=?;", string(pwdHash), record.Rating, record.RealName, record.CID)
		if err != nil {
			return err
		}
	}

	return nil
}

func IsUsersTableEmpty(db *sql.DB) (bool, error) {
	row := db.QueryRow("SELECT * FROM users LIMIT 1;")

	var cid int
	var pwd string
	var rating int
	var realName string
	var creationTime time.Time

	if err := row.Scan(&cid, &pwd, &rating, &realName, &creationTime); errors.Is(err, sql.ErrNoRows) {
		return true, nil
	} else {
		return false, err
	}
}
