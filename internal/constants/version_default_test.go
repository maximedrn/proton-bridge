// Copyright (c) 2026 Proton AG
//
// This file is part of Proton Mail Bridge.
//
// Proton Mail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Proton Mail Bridge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

//go:build !build_qa

package constants

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testBridgeVersion = "3.27.1+git"
	testReleasePath   = "/ProtonMail/android-mail/releases/tag/7.11.5(18317)"
	testReleaseHeader = "other@7.11.5"
	testLatestPath    = "/ProtonMail/android-mail/releases/latest"
)

type versionRoundTripper func(*http.Request) (*http.Response, error)

func (transport versionRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func resetVersionLookup(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	previousTransport, previousVersion := http.DefaultTransport, versionCache
	http.DefaultTransport, versionCache = transport, ""
	t.Cleanup(func() {
		http.DefaultTransport, versionCache = previousTransport, previousVersion
	})
}

func serveVersionLookup(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	resetVersionLookup(t, versionRoundTripper(func(request *http.Request) (*http.Response, error) {
		localRequest := request.Clone(request.Context())
		localRequest.URL.Scheme, localRequest.URL.Host = serverURL.Scheme, serverURL.Host
		return server.Client().Transport.RoundTrip(localRequest)
	}))
}

func TestAppVersionCachesConcurrentLookups(t *testing.T) {
	var lookups atomic.Int32
	serveVersionLookup(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == testLatestPath {
			lookups.Add(1)
			time.Sleep(10 * time.Millisecond)
			http.Redirect(writer, request, testReleasePath, http.StatusFound)
		}
	})

	const callers = 16
	headers := make(chan string, callers)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range callers {
		workers.Go(func() {
			<-start
			headers <- AppVersion(testBridgeVersion)
		})
	}
	close(start)
	workers.Wait()
	close(headers)
	for header := range headers {
		require.Equal(t, testReleaseHeader, header)
	}
	require.EqualValues(t, 1, lookups.Load())
	require.Equal(t, testReleaseHeader, AppVersion(testBridgeVersion))
	require.EqualValues(t, 1, lookups.Load())
}

func TestAppVersionFallsBackWhenReleaseLookupFails(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		status     int
		redirectTo string
	}{
		{name: "unavailable", status: http.StatusServiceUnavailable},
		{name: "missing release tag", status: http.StatusOK},
		{name: "invalid release tag", redirectTo: "/ProtonMail/android-mail/releases/tag/not-a-version"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var lookups atomic.Int32
			serveVersionLookup(t, func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == testLatestPath {
					lookups.Add(1)
					if scenario.redirectTo != "" {
						http.Redirect(writer, request, scenario.redirectTo, http.StatusFound)
						return
					}
					writer.WriteHeader(scenario.status)
				}
			})
			require.Equal(t, "other@3.27.1+git", AppVersion(testBridgeVersion))
			require.Equal(t, "other@3.27.1+git", AppVersion(testBridgeVersion))
			require.EqualValues(t, 1, lookups.Load())
		})
	}
}

func TestAppVersionBoundsNetworkRequests(t *testing.T) {
	var deadline time.Time
	resetVersionLookup(t, versionRoundTripper(func(request *http.Request) (*http.Response, error) {
		deadline, _ = request.Context().Deadline()
		return nil, errors.New("network unavailable")
	}))
	require.Equal(t, "other@3.27.1+git", AppVersion(testBridgeVersion))
	require.False(t, deadline.IsZero(), "release lookup must have a deadline")
	require.Positive(t, time.Until(deadline))
	require.Less(t, time.Until(deadline), time.Minute)
}

func TestNewestAppVersionAcceptsReleaseTagFormats(t *testing.T) {
	for _, scenario := range []struct {
		path    string
		version string
	}{
		{path: testReleasePath, version: "7.11.5"},
		{path: "/ProtonMail/android-mail/releases/tag/v6.0.0", version: "6.0.0"},
	} {
		t.Run(scenario.version, func(t *testing.T) {
			serveVersionLookup(t, func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == testLatestPath {
					http.Redirect(writer, request, scenario.path, http.StatusFound)
				}
			})
			version, err := getNewestAppVersion()
			require.NoError(t, err)
			require.Equal(t, scenario.version, version)
		})
	}
}
