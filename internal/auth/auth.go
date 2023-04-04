package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bloqs-sites/bloqsenjin/pkg/auth"
	"github.com/bloqs-sites/bloqsenjin/pkg/conf"
	"github.com/bloqs-sites/bloqsenjin/pkg/db"
	"github.com/bloqs-sites/bloqsenjin/proto"
	"github.com/golang-jwt/jwt/v4"
	"golang.org/x/crypto/bcrypt"
)

var (
	email_regex *regexp.Regexp

	hostname string

	domains_list = conf.MustGetConfOrDefault[map[string][]string](nil, "domains")
)

const (
	email_regex_str = "(?:[a-z0-9!#$%&'*+/=?^_`{|}~-]+(?:\\.[a-z0-9!#$%&'*+/=?^_`{|}~-]+)*|\"(?:[\\x01-\\x08\\x0b\\x0c\\x0e-\\x1f\\x21\\x23-\\x5b\\x5d-\\x7f]|\\[\\x01-\\x09\\x0b\\x0c\\x0e-\\x7f])*\")@(?:(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\\.)+[a-z0-9](?:[a-z0-9-]*[a-z0-9])?|\\[(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?|[a-z0-9-]*[a-z0-9]:(?:[\\x01-\\x08\\x0b\\x0c\\x0e-\\x1f\\x21-\\x5a\\x53-\\x7f]|\\[\\x01-\\x09\\x0b\\x0c\\x0e-\\x7f])+)\\])"

	domains_blacklist = iota
	domains_whitelist
	domains_nil

	basic_email_prefix = "creds:basic:email:%s"
	jwt_prefix         = "token:jwt:%s"
)

func init() {
	regex, err := regexp.Compile(email_regex_str)
	if err != nil {
		panic(err)
	}

	email_regex = regex

	hostname, err = os.Hostname()
	if err != nil {
		panic(err)
	}
}

type claims struct {
	auth.Payload
	jwt.RegisteredClaims
}

type BloqsAuther struct {
	creds   db.KVDBer
}
func NewBloqsAuther(creds db.KVDBer) *BloqsAuther {
	return &BloqsAuther{creds}
}

func (a *BloqsAuther) SignInBasic(ctx context.Context, c *proto.Credentials_Basic) error {
	if err := verifyEmail(c.Basic.GetEmail()); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(c.Basic.GetPassword()), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return a.creds.Put(ctx, map[string][]byte{
		fmt.Sprintf(basic_email_prefix, c.Basic.GetEmail()): hash,
	}, 0)
}

func (a *BloqsAuther) SignOutBasic(ctx context.Context, c *proto.Credentials_Basic, tk *proto.Token, t *auth.Tokener) error {
	if err := a.CheckAccessBasic(ctx, c); err != nil {
		return err
	}

	if err := a.creds.Delete(ctx, fmt.Sprintf(basic_email_prefix, c.Basic.GetEmail())); err != nil {
		return err
	}

	return nil
}

func (a *BloqsAuther) GrantTokenBasic(ctx context.Context, c *proto.Credentials_Basic, p auth.Permissions, t auth.Tokener) (auth.Token, error) {
	if err := a.CheckAccessBasic(ctx, c); err != nil {
		return "", err
	}

    return t.GenToken(ctx, &auth.Payload{
        Client: c.Basic.Email,
        Permissions: p,
    })
}

func (a *BloqsAuther) CheckAccessBasic(ctx context.Context, c *proto.Credentials_Basic) error {
	hashes, err := a.creds.Get(ctx, fmt.Sprintf(basic_email_prefix, c.Basic.GetEmail()))

	if err != nil {
		return err
	}

	hash := hashes[fmt.Sprintf(basic_email_prefix, c.Basic.GetEmail())]

	if err := bcrypt.CompareHashAndPassword(hash, []byte(c.Basic.GetPassword())); err != nil {
		return err
	}

	return nil
}

func verifyEmail(email string) error {
	valid := email_regex.Match([]byte(email))

	if !valid {
		return fmt.Errorf("The email `%s` has an invalid format", email)
	}

	email_domain := strings.Split(email, "@")[1]

	v, t := getDomainsListType()
	switch t {
	case domains_nil:
	case domains_blacklist:
		for _, d := range v {
			if d == email_domain {
				return fmt.Errorf("The email `%s` has a blacklisted domain", email)
			}
		}
	case domains_whitelist:
		for _, d := range v {
			if d == email_domain {
				break
			}
		}
		return fmt.Errorf("The email `%s` has a non whitelisted domain", email)
	}

	mxr, err := net.LookupMX(email_domain)

	if err != nil {
		return err
	}

	sort.SliceStable(mxr, func(i, j int) bool {
		return mxr[i].Pref < mxr[j].Pref
	})

	ch, n := make(chan errorCloseClosure, len(mxr)+1), 0

	for _, i := range mxr {
		switch t {
		case domains_blacklist:
			for _, d := range v {
				if d == i.Host {
					return fmt.Errorf("The email `%s` has a blacklisted domain", email)
				}
			}
		}
		go smtpVerify(ch, email, i.Host)
	}
	go smtpVerify(ch, email, email_domain)

	for {
		select {
		case c := <-ch:
			n++
			err := c.close()
			if c.err == nil && err == nil {
				return nil
			}

			fmt.Println(err, c.err)

			if n >= len(mxr)+1 {
				return fmt.Errorf("The email `%s` is invalid", email)
			}
		}
	}
}

func getDomainsListType() ([]string, int) {
	if domains_list == nil {
		return nil, domains_nil
	}

	if v, ok := domains_list["blacklist"]; ok {
		return v, domains_blacklist
	} else if v, ok := domains_list["whitelist"]; ok {
		return v, domains_whitelist
	}

	return nil, domains_nil
}

type errorCloseClosure struct {
	err   error
	close func() error
}

func newErrorCloseClosure(err error, con net.Conn) errorCloseClosure {
	var close func() error
	if con != nil {
		close = con.Close
	} else {
		close = func() error {
			return err
		}
	}

	return errorCloseClosure{
		err,
		close,
	}
}
func smtpVerify(ch chan errorCloseClosure, email, mx string) {
	con, err := net.Dial("tcp", net.JoinHostPort(mx, strconv.Itoa(25)))

	if err != nil {
		ch <- newErrorCloseClosure(err, con)
		return
	}

	stream := make([]byte, 998)
	status := stream[:3]

	fmt.Fprintf(con, "HELO %s\r\n", hostname)
	bufio.NewReader(con).Read(stream)

	if string(status) != "220" {
		ch <- newErrorCloseClosure(errors.New("Service not ready"), con)
		return
	}

	fmt.Fprintf(con, "MAIL FROM: <%s>\r\n", "example@example.org")
	bufio.NewReader(con).Read(stream)

	if string(status) != "250" {
		ch <- newErrorCloseClosure(errors.New(""), con)
		return
	}

	fmt.Fprintf(con, "RCPT TO: <%s>\r\n", email)
	bufio.NewReader(con).Read(stream)

	if string(status) != "250" {
		ch <- newErrorCloseClosure(errors.New(""), con)
		return
	}

	fmt.Fprintf(con, "RSET\r\n")
	bufio.NewReader(con).Read(stream)

	if string(status) != "250" {
		ch <- newErrorCloseClosure(errors.New(""), con)
		return
	}

	fmt.Fprintf(con, "QUIT\r\n")
	bufio.NewReader(con).Read(stream)

	if string(status) != "221" && string(status) != "250" {
		ch <- newErrorCloseClosure(errors.New(""), con)
		return
	}

	ch <- newErrorCloseClosure(nil, con)
}
