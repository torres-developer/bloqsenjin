package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/bloqs-sites/bloqsenjin/pkg/auth"
	"github.com/bloqs-sites/bloqsenjin/pkg/db"
	"github.com/bloqs-sites/bloqsenjin/pkg/email"
	mux "github.com/bloqs-sites/bloqsenjin/pkg/http"
	"github.com/bloqs-sites/bloqsenjin/proto"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	jwt_prefix    = "token:jwt:%s"
	table         = "credentials"
	id_type_table = "id-type"
	failed_table  = "failed"
)

type claims struct {
	auth.Payload
	jwt.RegisteredClaims
}

type BloqsAuther struct {
	creds db.DataManipulater
}

func NewBloqsAuther(ctx context.Context, creds db.DataManipulater) (*BloqsAuther, error) {
	err := creds.CreateTables(ctx, []db.Table{
		{
			Name: table,
			Columns: []string{
				"`id` INTEGER PRIMARY KEY AUTO_INCREMENT",
				"`identifier` VARCHAR(320) NOT NULL",
				"`type` INT NOT NULL",
				"`secret` TEXT NOT NULL",
				"`is_super` BOOLEAN NOT NULL DEFAULT 0",
				"`created_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP",
				"`modified_at` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP",
				"`last_log_in` TIMESTAMP",
				"UNIQUE (`identifier`, `type`)",
			},
		},
		{
			Name: "failed",
			Columns: []string{
				"`id` INTEGER PRIMARY KEY AUTO_INCREMENT",
				"`credential` INTEGER NOT NULL",
				"`timestamp` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP",
				//fmt.Sprintf("FOREIGN KEY (`credential`) REFERENCES `%s`(`id`)", table),
			},
		},
	})

	if err != nil {
		return nil, err
	}

	return &BloqsAuther{creds}, nil
}

func (a *BloqsAuther) SignInBasic(ctx context.Context, c *proto.Credentials_Basic) error {
	if err := email.VerifyEmail(ctx, c.Basic.Email); err != nil {
		status := uint16(http.StatusInternalServerError)

		switch err := err.(type) {
		case *email.InvalidEmailError:
			status = err.Status
		case *email.ServerError:
			status = uint16(http.StatusInternalServerError)
		}

		return &mux.HttpError{
			Body:   err.Error(),
			Status: status,
		}
	}

	pass := c.Basic.Password

	if len(pass) > 72 { // bcrypt says that "GenerateFromPassword does not accept passwords longer than 72 bytes"
		return errors.New("the password provided it's too long (bigger than 72 bytes)")
	}

	// TODO: test password entropy

	exists, err := a.creds.Select(ctx, table, func() map[string]any {
		return map[string]any{
			"identifier": new(string),
			"type":       new(int),
		}
	}, map[string]any{
		"identifier": c.Basic.Email,
		"type":       strconv.Itoa(int(auth.BASIC_EMAIL)),
	})

	if err != nil {
		return err
	}

	if len(exists.Rows) > 0 {
		return &mux.HttpError{
			Body:   "credentials already in use",
			Status: http.StatusConflict,
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if _, err := a.creds.Insert(ctx, table, []map[string]string{
		{
			"identifier": c.Basic.Email,
			"type":       strconv.Itoa(int(auth.BASIC_EMAIL)),
			"secret":     string(hash),
		},
	}); err != nil {
		return err
	}

	return nil
}

func (a *BloqsAuther) SignOutBasic(ctx context.Context, c *proto.Credentials_Basic, tk *proto.Token, t auth.Tokener) error {
	if err := a.CheckAccessBasic(ctx, c); err != nil {
		return err
	}

	if _, err := a.creds.Delete(ctx, table, []map[string]any{
		{
			"identifier": c.Basic.Email,
			"type":       strconv.Itoa(int(auth.BASIC_EMAIL)),
		},
	}); err != nil {
		return err
	}

	return nil
}

func (a *BloqsAuther) GrantTokenBasic(ctx context.Context, c *proto.Credentials_Basic, p auth.Permissions, t auth.Tokener) (tk auth.Token, err error) {
	if err = a.CheckAccessBasic(ctx, c); err != nil {
		return
	}

	tk, err = t.GenToken(ctx, &auth.Payload{
		Client: *auth.CredentialsToID(&proto.Credentials{
			Credentials: c,
		}),
		Permissions: p,
	})

	return
}

func (a *BloqsAuther) CheckAccessBasic(ctx context.Context, c *proto.Credentials_Basic) error {
	res, err := a.creds.Select(ctx, table, func() map[string]any {
		return map[string]any{
			"secret": new([]byte),
		}
	}, map[string]any{
		"identifier": c.Basic.Email,
		"type":       strconv.Itoa(int(auth.BASIC_EMAIL)),
	})

	if err != nil {
		return &mux.HttpError{
			Body:   err.Error(),
			Status: http.StatusInternalServerError,
		}
	}

	if len(res.Rows) != 1 {
		return &mux.HttpError{
			Body:   "wrong credentials",
			Status: http.StatusUnauthorized,
		}
	}

	secret := res.Rows[0]["secret"].(*[]byte)
	if err := bcrypt.CompareHashAndPassword(*secret, []byte(c.Basic.GetPassword())); err != nil {
		return &mux.HttpError{
			Body:   "wrong credentials",
			Status: http.StatusUnauthorized,
		}
	}

	return nil
}

func (a *BloqsAuther) RevokeToken(ctx context.Context, tk *proto.Token, t auth.Tokener) error {
	return t.RevokeToken(ctx, auth.Token(tk.Jwt))
}
