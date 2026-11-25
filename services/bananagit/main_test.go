// Copyright 2026 The BananaGit Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package bananagit

import (
	"net/http"
	"sync"
	"testing"

	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/session"
	"gitea.dev/services/auth/source/oauth2"

	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

// memStore is a session store in memory.
type memStore struct {
	mu        sync.Mutex
	data      map[any]any
	destroyed bool
}

func newMemStore(values map[string]any) *memStore {
	s := &memStore{data: map[any]any{}}
	for k, v := range values {
		s.data[k] = v
	}
	return s
}

func (s *memStore) Set(k, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[k] = v
	return nil
}

func (s *memStore) Get(k any) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[k]
}

func (s *memStore) Delete(k any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, k)
	return nil
}

func (s *memStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = map[any]any{}
	return nil
}

func (s *memStore) ID() string     { return "test-session" }

func (s *memStore) Release() error { return nil }

func (s *memStore) Destroy(http.ResponseWriter, *http.Request) error {
	s.destroyed = true
	return s.Flush()
}

var _ session.Store = &memStore{}

func addHostingSource(t *testing.T, tokenURL string) *auth_model.Source {
	source := &auth_model.Source{
		Type:     auth_model.OAuth2,
		Name:     "BananaWiki",
		IsActive: true,
		Cfg: &oauth2.Source{
			Provider: HostingProviderName, ClientID: "bwa_x", ClientSecret: "s",
			CustomURLMapping: &oauth2.CustomURLMapping{AuthURL: tokenURL, TokenURL: tokenURL, ProfileURL: tokenURL},
		},
	}
	require.NoError(t, auth_model.CreateSource(t.Context(), source))
	return source
}
