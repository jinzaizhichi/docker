package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"testing"

	"github.com/moby/moby/api/types"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	"gotest.tools/v3/skip"
)

func TestNewClientWithOpsFromEnv(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows")

	testcases := []struct {
		doc             string
		envs            map[string]string
		expectedError   string
		expectedVersion string
	}{
		{
			doc:             "default api version",
			envs:            map[string]string{},
			expectedVersion: DefaultAPIVersion,
		},
		{
			doc: "invalid cert path",
			envs: map[string]string{
				"DOCKER_CERT_PATH": "invalid/path",
			},
			expectedError: "could not load X509 key pair: open invalid/path/cert.pem: no such file or directory",
		},
		{
			doc: "default api version with cert path",
			envs: map[string]string{
				"DOCKER_CERT_PATH": "testdata/",
			},
			expectedVersion: DefaultAPIVersion,
		},
		{
			doc: "default api version with cert path and tls verify",
			envs: map[string]string{
				"DOCKER_CERT_PATH":  "testdata/",
				"DOCKER_TLS_VERIFY": "1",
			},
			expectedVersion: DefaultAPIVersion,
		},
		{
			doc: "default api version with cert path and host",
			envs: map[string]string{
				"DOCKER_CERT_PATH": "testdata/",
				"DOCKER_HOST":      "https://notaunixsocket",
			},
			expectedVersion: DefaultAPIVersion,
		},
		{
			doc: "invalid docker host",
			envs: map[string]string{
				"DOCKER_HOST": "host",
			},
			expectedError: "unable to parse docker host `host`",
		},
		{
			doc: "invalid docker host, with good format",
			envs: map[string]string{
				"DOCKER_HOST": "invalid://url",
			},
			expectedVersion: DefaultAPIVersion,
		},
		{
			doc: "override api version",
			envs: map[string]string{
				"DOCKER_API_VERSION": "1.22",
			},
			expectedVersion: "1.22",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.doc, func(t *testing.T) {
			for key, value := range tc.envs {
				t.Setenv(key, value)
			}
			client, err := NewClientWithOpts(FromEnv)
			if tc.expectedError != "" {
				assert.Check(t, is.Error(err, tc.expectedError))
			} else {
				assert.NilError(t, err)
				assert.Check(t, is.Equal(client.ClientVersion(), tc.expectedVersion))
			}

			if tc.envs["DOCKER_TLS_VERIFY"] != "" {
				// pedantic checking that this is handled correctly
				tlsConfig := client.tlsConfig()
				assert.Assert(t, tlsConfig != nil)
				assert.Check(t, is.Equal(tlsConfig.InsecureSkipVerify, false))
			}
		})
	}
}

func TestGetAPIPath(t *testing.T) {
	tests := []struct {
		version  string
		path     string
		query    url.Values
		expected string
	}{
		{
			path:     "/containers/json",
			expected: "/v" + DefaultAPIVersion + "/containers/json",
		},
		{
			path:     "/containers/json",
			query:    url.Values{},
			expected: "/v" + DefaultAPIVersion + "/containers/json",
		},
		{
			path:     "/containers/json",
			query:    url.Values{"s": []string{"c"}},
			expected: "/v" + DefaultAPIVersion + "/containers/json?s=c",
		},
		{
			version:  "1.22",
			path:     "/containers/json",
			expected: "/v1.22/containers/json",
		},
		{
			version:  "1.22",
			path:     "/containers/json",
			query:    url.Values{},
			expected: "/v1.22/containers/json",
		},
		{
			version:  "1.22",
			path:     "/containers/json",
			query:    url.Values{"s": []string{"c"}},
			expected: "/v1.22/containers/json?s=c",
		},
		{
			version:  "v1.22",
			path:     "/containers/json",
			expected: "/v1.22/containers/json",
		},
		{
			version:  "v1.22",
			path:     "/containers/json",
			query:    url.Values{},
			expected: "/v1.22/containers/json",
		},
		{
			version:  "v1.22",
			path:     "/containers/json",
			query:    url.Values{"s": []string{"c"}},
			expected: "/v1.22/containers/json?s=c",
		},
		{
			version:  "v1.22",
			path:     "/networks/kiwl$%^",
			expected: "/v1.22/networks/kiwl$%25%5E",
		},
	}

	ctx := context.TODO()
	for _, tc := range tests {
		client, err := NewClientWithOpts(
			WithVersion(tc.version),
			WithHost("tcp://localhost:2375"),
		)
		assert.NilError(t, err)
		actual := client.getAPIPath(ctx, tc.path, tc.query)
		assert.Check(t, is.Equal(actual, tc.expected))
	}
}

func TestParseHostURL(t *testing.T) {
	testcases := []struct {
		host        string
		expected    *url.URL
		expectedErr string
	}{
		{
			host:        "",
			expectedErr: "unable to parse docker host",
		},
		{
			host:        "foobar",
			expectedErr: "unable to parse docker host",
		},
		{
			host:     "foo://bar",
			expected: &url.URL{Scheme: "foo", Host: "bar"},
		},
		{
			host:     "tcp://localhost:2476",
			expected: &url.URL{Scheme: "tcp", Host: "localhost:2476"},
		},
		{
			host:     "tcp://localhost:2476/path",
			expected: &url.URL{Scheme: "tcp", Host: "localhost:2476", Path: "/path"},
		},
		{
			host:     "unix:///var/run/docker.sock",
			expected: &url.URL{Scheme: "unix", Host: "/var/run/docker.sock"},
		},
		{
			host:     "npipe:////./pipe/docker_engine",
			expected: &url.URL{Scheme: "npipe", Host: "//./pipe/docker_engine"},
		},
	}

	for _, testcase := range testcases {
		actual, err := ParseHostURL(testcase.host)
		if testcase.expectedErr != "" {
			assert.Check(t, is.ErrorContains(err, testcase.expectedErr))
		}
		assert.Check(t, is.DeepEqual(actual, testcase.expected))
	}
}

func TestNewClientWithOpsFromEnvSetsDefaultVersion(t *testing.T) {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_API_VERSION", "")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	client, err := NewClientWithOpts(FromEnv)
	assert.NilError(t, err)
	assert.Check(t, is.Equal(client.ClientVersion(), DefaultAPIVersion))

	const expected = "1.22"
	t.Setenv("DOCKER_API_VERSION", expected)
	client, err = NewClientWithOpts(FromEnv)
	assert.NilError(t, err)
	assert.Check(t, is.Equal(client.ClientVersion(), expected))
}

// TestNegotiateAPIVersionEmpty asserts that client.Client version negotiation
// downgrades to the correct API version if the API's ping response does not
// return an API version.
func TestNegotiateAPIVersionEmpty(t *testing.T) {
	t.Setenv("DOCKER_API_VERSION", "")

	client, err := NewClientWithOpts(FromEnv)
	assert.NilError(t, err)

	// set our version to something new
	client.version = "1.25"

	// if no version from server, expect the earliest
	// version before APIVersion was implemented
	const expected = "1.24"

	// test downgrade
	client.NegotiateAPIVersionPing(types.Ping{})
	assert.Check(t, is.Equal(client.ClientVersion(), expected))
}

// TestNegotiateAPIVersion asserts that client.Client can
// negotiate a compatible APIVersion with the server
func TestNegotiateAPIVersion(t *testing.T) {
	tests := []struct {
		doc             string
		clientVersion   string
		pingVersion     string
		expectedVersion string
	}{
		{
			// client should downgrade to the version reported by the daemon.
			doc:             "downgrade from default",
			pingVersion:     "1.21",
			expectedVersion: "1.21",
		},
		{
			// client should not downgrade to the version reported by the
			// daemon if a custom version was set.
			doc:             "no downgrade from custom version",
			clientVersion:   "1.25",
			pingVersion:     "1.21",
			expectedVersion: "1.25",
		},
		{
			// client should downgrade to the last version before version
			// negotiation was added (1.24) if the daemon does not report
			// a version.
			doc:             "downgrade legacy",
			pingVersion:     "",
			expectedVersion: "1.24",
		},
		{
			// client should downgrade to the version reported by the daemon.
			// version negotiation was added in API 1.25, so this is theoretical,
			// but it should negotiate to versions before that if the daemon
			// gives that as a response.
			doc:             "downgrade old",
			pingVersion:     "1.19",
			expectedVersion: "1.19",
		},
		{
			// client should not upgrade to a newer version if a version was set,
			// even if both the daemon and the client support it.
			doc:             "no upgrade",
			clientVersion:   "1.20",
			pingVersion:     "1.21",
			expectedVersion: "1.20",
		},
	}

	for _, tc := range tests {
		t.Run(tc.doc, func(t *testing.T) {
			opts := make([]Opt, 0)
			if tc.clientVersion != "" {
				// Note that this check is redundant, as WithVersion() considers
				// an empty version equivalent to "not setting a version", but
				// doing this just to be explicit we are using the default.
				opts = append(opts, WithVersion(tc.clientVersion))
			}
			client, err := NewClientWithOpts(opts...)
			assert.NilError(t, err)
			client.NegotiateAPIVersionPing(types.Ping{APIVersion: tc.pingVersion})
			assert.Check(t, is.Equal(tc.expectedVersion, client.ClientVersion()))
		})
	}
}

// TestNegotiateAPIVersionOverride asserts that we honor the DOCKER_API_VERSION
// environment variable when negotiating versions.
func TestNegotiateAPIVersionOverride(t *testing.T) {
	const expected = "9.99"
	t.Setenv("DOCKER_API_VERSION", expected)

	client, err := NewClientWithOpts(FromEnv)
	assert.NilError(t, err)

	// test that we honored the env var
	client.NegotiateAPIVersionPing(types.Ping{APIVersion: "1.24"})
	assert.Check(t, is.Equal(client.ClientVersion(), expected))
}

// TestNegotiateAPIVersionConnectionFailure asserts that we do not modify the
// API version when failing to connect.
func TestNegotiateAPIVersionConnectionFailure(t *testing.T) {
	const expected = "9.99"

	client, err := NewClientWithOpts(WithHost("tcp://no-such-host.invalid"))
	assert.NilError(t, err)

	client.version = expected
	client.NegotiateAPIVersion(context.Background())
	assert.Check(t, is.Equal(client.ClientVersion(), expected))
}

func TestNegotiateAPIVersionAutomatic(t *testing.T) {
	var pingVersion string
	httpClient := newMockClient(func(req *http.Request) (*http.Response, error) {
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}
		resp.Header.Set("Api-Version", pingVersion)
		resp.Body = io.NopCloser(strings.NewReader("OK"))
		return resp, nil
	})

	ctx := context.Background()
	client, err := NewClientWithOpts(
		WithHTTPClient(httpClient),
		WithAPIVersionNegotiation(),
	)
	assert.NilError(t, err)

	// Client defaults to use DefaultAPIVersion before version-negotiation.
	expected := DefaultAPIVersion
	assert.Check(t, is.Equal(client.ClientVersion(), expected))

	// First request should trigger negotiation
	pingVersion = "1.35"
	expected = "1.35"
	_, _ = client.Info(ctx)
	assert.Check(t, is.Equal(client.ClientVersion(), expected))

	// Once successfully negotiated, subsequent requests should not re-negotiate
	pingVersion = "1.25"
	expected = "1.35"
	_, _ = client.Info(ctx)
	assert.Check(t, is.Equal(client.ClientVersion(), expected))
}

// TestNegotiateAPIVersionWithEmptyVersion asserts that initializing a client
// with an empty version string does still allow API-version negotiation
func TestNegotiateAPIVersionWithEmptyVersion(t *testing.T) {
	client, err := NewClientWithOpts(WithVersion(""))
	assert.NilError(t, err)

	const expected = "1.35"
	client.NegotiateAPIVersionPing(types.Ping{APIVersion: expected})
	assert.Check(t, is.Equal(client.ClientVersion(), expected))
}

// TestNegotiateAPIVersionWithFixedVersion asserts that initializing a client
// with a fixed version disables API-version negotiation
func TestNegotiateAPIVersionWithFixedVersion(t *testing.T) {
	const customVersion = "1.35"
	client, err := NewClientWithOpts(WithVersion(customVersion))
	assert.NilError(t, err)

	client.NegotiateAPIVersionPing(types.Ping{APIVersion: "1.31"})
	assert.Check(t, is.Equal(client.ClientVersion(), customVersion))
}

// TestCustomAPIVersion tests initializing the client with a custom
// version.
func TestCustomAPIVersion(t *testing.T) {
	tests := []struct {
		version  string
		expected string
	}{
		{
			version:  "",
			expected: DefaultAPIVersion,
		},
		{
			version:  "1.0",
			expected: "1.0",
		},
		{
			version:  "9.99",
			expected: "9.99",
		},
		{
			version:  "v",
			expected: DefaultAPIVersion,
		},
		{
			version:  "v1.0",
			expected: "1.0",
		},
		{
			version:  "v9.99",
			expected: "9.99",
		},
		{
			// When manually setting a version, no validation happens.
			// so anything is accepted.
			version:  "something-weird",
			expected: "something-weird",
		},
	}
	for _, tc := range tests {
		t.Run(tc.version, func(t *testing.T) {
			client, err := NewClientWithOpts(WithVersion(tc.version))
			assert.NilError(t, err)
			assert.Check(t, is.Equal(client.ClientVersion(), tc.expected))

			t.Setenv(EnvOverrideAPIVersion, tc.expected)
			client, err = NewClientWithOpts(WithVersionFromEnv())
			assert.NilError(t, err)
			assert.Check(t, is.Equal(client.ClientVersion(), tc.expected))
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (rtf roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return rtf(req)
}

type bytesBufferClose struct {
	*bytes.Buffer
}

func (bbc bytesBufferClose) Close() error {
	return nil
}

func TestClientRedirect(t *testing.T) {
	client := &http.Client{
		CheckRedirect: CheckRedirect,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() == "/bla" {
				return &http.Response{StatusCode: http.StatusNotFound}, nil
			}
			return &http.Response{
				StatusCode: http.StatusMovedPermanently,
				Header:     http.Header{"Location": {"/bla"}},
				Body:       bytesBufferClose{bytes.NewBuffer(nil)},
			}, nil
		}),
	}

	tests := []struct {
		httpMethod  string
		expectedErr *url.Error
		statusCode  int
	}{
		{
			httpMethod: http.MethodGet,
			statusCode: http.StatusMovedPermanently,
		},
		{
			httpMethod:  http.MethodPost,
			expectedErr: &url.Error{Op: "Post", URL: "/bla", Err: ErrRedirect},
			statusCode:  http.StatusMovedPermanently,
		},
		{
			httpMethod:  http.MethodPut,
			expectedErr: &url.Error{Op: "Put", URL: "/bla", Err: ErrRedirect},
			statusCode:  http.StatusMovedPermanently,
		},
		{
			httpMethod:  http.MethodDelete,
			expectedErr: &url.Error{Op: "Delete", URL: "/bla", Err: ErrRedirect},
			statusCode:  http.StatusMovedPermanently,
		},
	}

	for _, tc := range tests {
		t.Run(tc.httpMethod, func(t *testing.T) {
			req, err := http.NewRequest(tc.httpMethod, "/redirectme", http.NoBody)
			assert.NilError(t, err)
			resp, err := client.Do(req)
			assert.Check(t, is.Equal(resp.StatusCode, tc.statusCode))
			if tc.expectedErr == nil {
				assert.NilError(t, err)
			} else {
				assert.Check(t, is.ErrorType(err, &url.Error{}))
				var urlError *url.Error
				assert.Check(t, errors.As(err, &urlError), "%T is not *url.Error", err)
				assert.Check(t, is.Equal(*urlError, *tc.expectedErr))
			}
		})
	}
}
