/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 * @author Denis Gribanov
 */

package coreutils

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/voedger/voedger/pkg/appdef"
	ibus "github.com/voedger/voedger/staging/src/github.com/untillpro/airs-ibus"
	"github.com/voedger/voedger/staging/src/github.com/untillpro/ibusmem"
)

func TestNewHTTPError(t *testing.T) {
	require := require.New(t)
	t.Run("simple", func(t *testing.T) {
		sysErr := NewHTTPError(http.StatusInternalServerError, errors.New("test error"))
		require.Empty(sysErr.Data)
		require.Equal(http.StatusInternalServerError, sysErr.HTTPStatus)
		require.Equal("test error", sysErr.Message)
		require.Equal(appdef.NullQName, sysErr.QName)
		require.Equal(`{"sys.Error":{"HTTPStatus":500,"Message":"test error"}}`, sysErr.ToJSON())
	})

	t.Run("formatted", func(t *testing.T) {
		sysErr := NewHTTPErrorf(http.StatusInternalServerError, "test ", "error")
		require.Empty(sysErr.Data)
		require.Equal(http.StatusInternalServerError, sysErr.HTTPStatus)
		require.Equal("test error", sysErr.Message)
		require.Equal(appdef.NullQName, sysErr.QName)
		require.Equal(`{"sys.Error":{"HTTPStatus":500,"Message":"test error"}}`, sysErr.ToJSON())
	})
}

type testResp struct {
	sender interface{}
	resp   ibus.Response
}

type testIBus struct {
	responses []testResp
}

func (bus *testIBus) SendRequest2(ctx context.Context, request ibus.Request, timeout time.Duration) (res ibus.Response, sections <-chan ibus.ISection, secError *error, err error) {
	panic("")
}

func (bus *testIBus) SendResponse(sender interface{}, response ibus.Response) {
	bus.responses = append(bus.responses, testResp{
		sender: sender,
		resp:   response,
	})
}

func (bus *testIBus) SendParallelResponse2(sender interface{}) (rsender ibus.IResultSenderClosable) {
	panic("")
}

func TestReply(t *testing.T) {
	require := require.New(t)
	busSender := "whatever"

	t.Run("ReplyErr", func(t *testing.T) {
		bus := &testIBus{}
		err := errors.New("test error")
		sender := ibusmem.NewISender(bus, busSender)
		ReplyErr(sender, err)
		expectedResp := ibus.Response{
			ContentType: ApplicationJSON,
			StatusCode:  http.StatusInternalServerError,
			Data:        []byte(`{"sys.Error":{"HTTPStatus":500,"Message":"test error"}}`),
		}
		require.Equal(testResp{sender: "whatever", resp: expectedResp}, bus.responses[0])
	})

	t.Run("ReplyErrf", func(t *testing.T) {
		bus := &testIBus{}
		sender := ibusmem.NewISender(bus, busSender)
		ReplyErrf(sender, http.StatusAccepted, "test ", "message")
		expectedResp := ibus.Response{
			ContentType: ApplicationJSON,
			StatusCode:  http.StatusAccepted,
			Data:        []byte(`{"sys.Error":{"HTTPStatus":202,"Message":"test message"}}`),
		}
		require.Equal(testResp{sender: "whatever", resp: expectedResp}, bus.responses[0])
	})

	t.Run("ReplyErrorDef", func(t *testing.T) {
		t.Run("common error", func(t *testing.T) {
			bus := &testIBus{}
			err := errors.New("test error")
			sender := ibusmem.NewISender(bus, busSender)
			ReplyErrDef(sender, err, http.StatusAccepted)
			expectedResp := ibus.Response{
				ContentType: ApplicationJSON,
				StatusCode:  http.StatusAccepted,
				Data:        []byte(`{"sys.Error":{"HTTPStatus":202,"Message":"test error"}}`),
			}
			require.Equal(testResp{sender: "whatever", resp: expectedResp}, bus.responses[0])
		})
		t.Run("SysError", func(t *testing.T) {
			bus := &testIBus{}
			err := SysError{
				HTTPStatus: http.StatusAlreadyReported,
				Message:    "test error",
				Data:       "dddfd",
				QName:      appdef.NewQName("my", "qname"),
			}
			sender := ibusmem.NewISender(bus, busSender)
			ReplyErrDef(sender, err, http.StatusAccepted)
			expectedResp := ibus.Response{
				ContentType: ApplicationJSON,
				StatusCode:  http.StatusAlreadyReported,
				Data:        []byte(`{"sys.Error":{"HTTPStatus":208,"Message":"test error","QName":"my.qname","Data":"dddfd"}}`),
			}
			require.Equal(testResp{sender: "whatever", resp: expectedResp}, bus.responses[0])
		})
	})

	t.Run("http status helpers", func(t *testing.T) {
		cases := []struct {
			statusCode      int
			f               func(sender ibus.ISender, message string)
			expectedMessage string
		}{
			{f: ReplyUnauthorized, statusCode: http.StatusUnauthorized},
			{f: ReplyBadRequest, statusCode: http.StatusBadRequest},
			{f: ReplyAccessDeniedForbidden, statusCode: http.StatusForbidden, expectedMessage: "access denied: test message"},
			{f: ReplyAccessDeniedUnauthorized, statusCode: http.StatusUnauthorized, expectedMessage: "access denied: test message"},
		}

		for _, c := range cases {
			name := runtime.FuncForPC(reflect.ValueOf(c.f).Pointer()).Name()
			name = name[strings.LastIndex(name, ".")+1:]
			t.Run(name, func(t *testing.T) {
				bus := &testIBus{}
				busSender := "whatever"
				sender := ibusmem.NewISender(bus, busSender)
				c.f(sender, "test message")
				expectedMessage := "test message"
				if len(c.expectedMessage) > 0 {
					expectedMessage = c.expectedMessage
				}
				expectedResp := ibus.Response{
					ContentType: ApplicationJSON,
					StatusCode:  c.statusCode,
					Data:        []byte(fmt.Sprintf(`{"sys.Error":{"HTTPStatus":%d,"Message":"%s"}}`, c.statusCode, expectedMessage)),
				}
				require.Equal(testResp{sender: "whatever", resp: expectedResp}, bus.responses[0])
			})
		}

		t.Run("ReplyInternalServerError", func(t *testing.T) {
			bus := &testIBus{}
			busSender := "whatever"
			err := errors.New("test error")
			sender := ibusmem.NewISender(bus, busSender)
			ReplyInternalServerError(sender, "test", err)
			expectedResp := ibus.Response{
				ContentType: ApplicationJSON,
				StatusCode:  http.StatusInternalServerError,
				Data:        []byte(`{"sys.Error":{"HTTPStatus":500,"Message":"test: test error"}}`),
			}
			require.Equal(testResp{sender: "whatever", resp: expectedResp}, bus.responses[0])
		})
	})
}

func TestHTTP(t *testing.T) {
	require := require.New(t)

	listener, err := net.Listen("tcp", ServerAddress(0))
	require.NoError(err)
	var handler func(w http.ResponseWriter, r *http.Request)
	server := &http.Server{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler(w, r)
		}),
	}
	done := make(chan interface{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			require.NoError(err)
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	federationURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	require.NoError(err)
	federation, cleanup := NewIFederation(func() *url.URL {
		return federationURL
	})
	defer cleanup()

	t.Run("basic", func(t *testing.T) {
		handler = func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(err)
			w.Write([]byte(fmt.Sprintf("hello, %s", string(body))))
		}
		resp, err := federation.POST("test", "world")
		require.NoError(err)
		require.Equal("hello, world", resp.Body)
		require.Equal(http.StatusOK, resp.HTTPResp.StatusCode)
	})

	t.Run("cookies & headers", func(t *testing.T) {
		handler = func(_ http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			require.NoError(err)
			// require.Len(r.Header, 2)
			require.Equal("headerValue", r.Header["Header-Key"][0])
			require.Equal("Bearer authorizationValue", r.Header["Authorization"][0])
		}
		resp, err := federation.POST("test", "world",
			WithCookies("cookieKey", "cookieValue"),
			WithHeaders("Header-Key", "headerValue"),
			WithAuthorizeBy("authorizationValue"),
		)
		require.NoError(err)
		fmt.Println(resp.Body)
	})

	require.NoError(server.Shutdown(context.Background()))

	<-done
}

func TestFederationFunc(t *testing.T) {
	require := require.New(t)

	listener, err := net.Listen("tcp", ServerAddress(0))
	require.NoError(err)
	var handler func(w http.ResponseWriter, r *http.Request)
	server := &http.Server{
		Addr: ":0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler(w, r)
		}),
	}
	done := make(chan interface{})
	go func() {
		defer close(done)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			require.NoError(err)
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	federationURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	require.NoError(err)
	federation, cleanup := NewIFederation(func() *url.URL {
		return federationURL
	})
	defer cleanup()

	t.Run("basic", func(t *testing.T) {
		handler = func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(err)
			require.Equal(`{"fld":"val"}`, string(body))
			w.Write([]byte(`{
				"NewIDs":{"1":2},
				"sections":[{"type":"","elements":[[[["hello, world"]]]]}],
				"CurrentWLogOffset":13,
				"Result":{"Int":42,"Str":"Str","sys.Container":"","sys.QName":"app1pkg.TestCmdResult"}
			}`))
		}
		resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`)
		require.NoError(err)
		resp.Println()
		require.Equal(int64(13), resp.CurrentWLogOffset)
		require.Equal("hello, world", resp.SectionRow()[0].(string))
		require.Equal(map[string]interface{}{
			"Int":           float64(42),
			"Str":           "Str",
			"sys.Container": "",
			"sys.QName":     "app1pkg.TestCmdResult",
		}, resp.CmdResult)
		require.Equal(int64(2), resp.NewID())
	})

	t.Run("unexpected error", func(t *testing.T) {
		cases := []struct {
			name        string
			handler     func(body string, w http.ResponseWriter, r *http.Request)
			expectedErr error
			opts        []ReqOptFunc
		}{
			{
				name: "basic error",
				handler: func(body string, w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"sys.Error":{"HTTPStatus":500,"Message":"something gone wrong","QName":"sys.SomeErrorQName","Data":"additional data"}}`))
				},
				expectedErr: FuncError{
					SysError: SysError{
						HTTPStatus: 500,
						QName:      appdef.NewQName("sys", "SomeErrorQName"),
						Message:    "something gone wrong",
						Data:       "additional data",
					},
					ExpectedHTTPCodes: []int{http.StatusOK},
				},
			},
			{
				name: "wrong QName",
				handler: func(body string, w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`{"sys.Error":{"HTTPStatus":500,"Message":"something gone wrong","QName":"errored QName","Data":"additional data"}}`))
				},
				expectedErr: FuncError{
					SysError: SysError{
						HTTPStatus: 500,
						QName:      appdef.NewQName("<err>", "errored QName"),
						Message:    "something gone wrong",
						Data:       "additional data",
					},
					ExpectedHTTPCodes: []int{http.StatusOK},
				},
			},
			{
				name: "wrong response JSON",
				handler: func(body string, w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte(`wrong JSON`))
				},
				expectedErr: errors.New("invalid character 'w' looking for beginning of value"),
			},
			{
				name: "non-OK status is expected",
				handler: func(body string, w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"sys.Error":{"HTTPStatus":500,"Message":"something gone wrong","QName":"errored QName","Data":"additional data"}}`))
				},
				expectedErr: FuncError{
					SysError: SysError{
						HTTPStatus: http.StatusOK,
					},
					ExpectedHTTPCodes: []int{http.StatusInternalServerError},
				},
				opts: []ReqOptFunc{WithExpectedCode(http.StatusInternalServerError)},
			},
		}

		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				handler = func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					require.NoError(err)
					c.handler(string(body), w, r)
				}
				resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`, c.opts...)
				var fe FuncError
				if errors.As(err, &fe) {
					require.Equal(c.expectedErr, err)
				} else {
					require.Equal(c.expectedErr.Error(), err.Error())
				}
				log.Println(err.Error())
				require.Nil(resp)
			})
		}
	})

	t.Run("expected error", func(t *testing.T) {
		t.Run("basic", func(t *testing.T) {
			handler = func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				require.NoError(err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"sys.Error":{"HTTPStatus":500,"Message":"something gone wrong","QName":"sys.SomeErrorQName","Data":"additional data"}}`))
			}
			resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`, WithExpectedCode(http.StatusInternalServerError))
			require.NoError(err)
			resp.Println()
			resp.RequireContainsError(t, "something")
			resp.RequireError(t, "something gone wrong")
		})
		t.Run("ExpectedErrorContains", func(t *testing.T) {
			errorMessage := "non-expected"
			handler = func(w http.ResponseWriter, r *http.Request) {
				_, err := io.ReadAll(r.Body)
				require.NoError(err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(fmt.Sprintf(`{"sys.Error":{"HTTPStatus":500,"Message":"%s","QName":"sys.SomeErrorQName","Data":"additional data"}}`,
					errorMessage)))
			}
			resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`, WithExpectedCode(http.StatusInternalServerError,
				"expected error message"))
			require.Error(err)
			require.Nil(resp)

			errorMessage = "expected error message"
			resp, err = federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`, WithExpectedCode(http.StatusInternalServerError,
				"expected error message"))
			require.NoError(err)
			resp.RequireContainsError(t, "expected")
			resp.RequireError(t, "expected error message")
		})
	})

	t.Run("sections", func(t *testing.T) {
		handler = func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			require.NoError(err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"sections":[{"type":"","elements":[[[["Hello", "world"]]],[[["next"]]]]}]}`))
		}
		resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`, WithExpectedCode(http.StatusInternalServerError))
		require.NoError(err)
		resp.Println()
		require.Equal("Hello", resp.SectionRow()[0].(string))
		require.Equal("world", resp.SectionRow()[1].(string))
		require.Equal("next", resp.SectionRow(1)[0].(string))
	})

	t.Run("automatic retry on 503", func(t *testing.T) {
		statusCode := http.StatusServiceUnavailable
		handler = func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			require.NoError(err)
			w.WriteHeader(statusCode)
			if statusCode == http.StatusOK {
				w.Write([]byte(`{"sections":[{"type":"","elements":[[[["Hello", "world"]]],[[["next"]]]]}]}`))
			}
			statusCode = http.StatusOK
		}
		resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`)
		require.NoError(err)
		resp.Println()
	})

	t.Run("discard response", func(t *testing.T) {
		handler = func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			require.NoError(err)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"sections":[{"type":"","elements":[[[["Hello", "world"]]],[[["next"]]]]}]}`))
		}
		resp, err := federation.Func("/api/123456789/c.sys.CUD", `{"fld":"val"}`, WithDiscardResponse())
		require.NoError(err)
		require.Nil(resp)
	})
}
