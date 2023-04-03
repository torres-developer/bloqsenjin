package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bloqs-sites/bloqsenjin/pkg/db"
	_ "github.com/go-sql-driver/mysql"
)

type MariaDB struct {
	conn *sql.DB
}

func NewMariaDB(dsn string) MariaDB {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}

	if err := db.Ping(); err != nil {
		panic(err)
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	return MariaDB{
		conn: db,
	}
}

func (dbh MariaDB) Select(table string, columns func() map[string]any) (db.Result, error) {
	r := make([]db.JSON, 0)

	column := columns()

	cl := len(column)
	if cl < 1 {
		return db.Result{
			LastID: nil,
			Rows:   r,
		}, nil
	}

	i, keys := 0, make([]string, cl)
	for k := range column {
		keys[i] = k
		i++
	}

	rows, err := dbh.conn.Query(fmt.Sprintf("SELECT %s FROM `%s`;", strings.Join(keys, ", "), table))

	if err != nil {
		return db.Result{
			LastID: nil,
			Rows:   r,
		}, err
	}

	defer rows.Close()

	if err != nil {
		return db.Result{
			LastID: nil,
			Rows:   r,
		}, err
	}

	for rows.Next() {
		loopc := columns()

		vals := make([]any, len(column))

		i := 0
		for _, v := range keys {
			vals[i] = loopc[v]
			i++
		}

		if err := rows.Scan(vals...); err != nil {
			return db.Result{
				LastID: nil,
				Rows:   r,
			}, err
		}

		row := make(db.JSON, len(column))

		i = 0
		for _, v := range keys {
			row[v] = vals[i]
			i++
		}

		r = append(r, row)
	}

	return db.Result{
		LastID: nil,
		Rows:   r,
	}, rows.Err()
}

func (dbh MariaDB) Insert(table string, rows []map[string]string) (db.Result, error) {
	if len(rows) < 1 {
		return db.Result{
			LastID: nil,
			Rows:   nil,
		}, errors.New("No rows to be inserted")
	}

	set := make(map[string]bool, len(rows[0]))
	for _, r := range rows {
		for c := range r {
			set[c] = true
		}
	}
	columns, i := make([]string, len(set)), 0
	for c := range set {
		columns[i] = c
		i++
	}

	rowsvals, i := make([][]any, len(rows)), 0
	for _, r := range rows {
		rowsvals[i] = make([]any, len(columns))
		for j, c := range columns {
			v, ok := r[c]

			if !ok {
				//rowsvals[i][j] = "DEFAULT"
				//rowsvals[i][j] = "NULL"
				//continue
				return db.Result{
					LastID: nil,
					Rows:   nil,
				}, errors.New("Cannot find value for column")
			}

			rowsvals[i][j] = v
		}
		i++
	}

	rowsstr := make([]string, len(rowsvals))
	vals, i := make([]any, len(rowsvals)*len(columns)), 0
	for j, r := range rowsvals {
		var rowstr strings.Builder
		rowstr.WriteString("(")
		first := true
		for _, v := range r {
			vals[i] = v
			i++
			if first {
				rowstr.WriteString("?")
				first = false
				continue
			}
			rowstr.WriteString(", ?")
		}
		rowstr.WriteString(")")
		rowsstr[j] = rowstr.String()
	}

	stmt := fmt.Sprintf("INSERT INTO `%s` (`%s`) VALUES %s", table, strings.Join(columns, "`, `"), strings.Join(rowsstr, ", "))

	res, err := dbh.conn.Exec(stmt, vals...)

	if err == nil {
		last, lasterr := res.LastInsertId()

		if lasterr != nil {
			return db.Result{
				LastID: nil,
				Rows:   nil,
			}, err
		}

		return db.Result{
			LastID: &last,
			Rows:   nil,
		}, err
	}

	return db.Result{
		LastID: nil,
		Rows:   nil,
	}, nil
}

func (dbh MariaDB) Update(table string, assignments []map[string]any, conditions []map[string]any) (db.Result, error) {
	r := make([]db.JSON, 0)
	return db.Result{
		LastID: nil,
		Rows:   r,
	}, nil
}

func (dbh MariaDB) Delete(table string, conditions []map[string]any) (db.Result, error) {
	r := make([]db.JSON, 0)
	return db.Result{
		LastID: nil,
		Rows:   r,
	}, nil
}

func (dbh MariaDB) CreateTables(ts []db.Table) error {
	for _, t := range ts {
		_, err := dbh.conn.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s`(%s);",
			t.Name, strings.Join(t.Columns, ", ")))

		if err != nil {
			fmt.Println(err)
			return err
		}
	}

	return nil
}

func (dbh MariaDB) CreateIndexes(ts []db.Index) error {
	return nil
}

func (dbh MariaDB) CreateViews(ts []db.View) error {
	for _, t := range ts {
		_, err := dbh.conn.Exec(fmt.Sprintf("CREATE OR REPLACE VIEW `%s` AS %s;",
			t.Name, t.Select))

		if err != nil {
			fmt.Println(err)
			return err
		}
	}

	return nil
}
