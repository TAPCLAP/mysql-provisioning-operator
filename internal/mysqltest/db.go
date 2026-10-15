package mysqltest

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"

	mysqlv1alpha1 "github.com/tapclap/mysql-provisioning-operator/api/v1alpha1"
	"github.com/tapclap/mysql-provisioning-operator/internal/mysql"
)

// Script is an in-memory MySQL session for tests.
type Script struct {
	mu        sync.Mutex
	Queries   []string
	QueryFunc func(query string) ([][]any, error)
	ExecFunc  func(query string) error
}

// Open implements mysql.Opener and returns the same script.
func (s *Script) Open(context.Context, mysqlv1alpha1.MySQLMember, mysql.Session) (mysql.DB, error) {
	return s, nil
}

// Exec records a statement.
func (s *Script) Exec(_ context.Context, query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.Contains(strings.ToUpper(query), "DROP DATABASE") || strings.Contains(strings.ToUpper(query), "DROP SCHEMA") {
		return fmt.Errorf("refusing to drop a database")
	}
	s.Queries = append(s.Queries, query)
	if s.ExecFunc != nil {
		return s.ExecFunc(query)
	}
	return nil
}

// Query records a statement and returns scripted rows.
func (s *Script) Query(_ context.Context, query string) (mysql.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Queries = append(s.Queries, query)
	if s.QueryFunc == nil {
		return &rows{}, nil
	}
	data, err := s.QueryFunc(query)
	if err != nil {
		return nil, err
	}
	return &rows{data: data}, nil
}

// Close implements mysql.DB.
func (s *Script) Close() error { return nil }

// Contains reports whether a recorded statement contains fragment.
func (s *Script) Contains(fragment string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, query := range s.Queries {
		if strings.Contains(strings.ToUpper(query), strings.ToUpper(fragment)) {
			return true
		}
	}
	return false
}

type rows struct {
	data [][]any
	i    int
}

func (r *rows) Next() bool {
	if r.i >= len(r.data) {
		return false
	}
	r.i++
	return true
}

func (r *rows) Scan(dest ...any) error {
	row := r.data[r.i-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d destinations, have %d columns", len(dest), len(row))
	}
	for i := range dest {
		if err := assign(dest[i], row[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *rows) Close() error { return nil }
func (r *rows) Err() error   { return nil }

func assign(dest, src any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return fmt.Errorf("scan destination must be a pointer")
	}
	if src == nil {
		return nil
	}
	sv := reflect.ValueOf(src)
	target := dv.Elem()
	if sv.Type().AssignableTo(target.Type()) {
		target.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(target.Type()) {
		target.Set(sv.Convert(target.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", src, dest)
}
