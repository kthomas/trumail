package api

import (
	"net/http"
	"strings"

	raven "github.com/getsentry/raven-go"
	"github.com/labstack/echo"
	tinystat "github.com/sdwolfe32/tinystat/client"
	"github.com/sdwolfe32/trumail/verifier"
)

const (
	FormatJSON  = "JSON"
	FormatJSONP = "JSONP"
	FormatXML   = "XML"
)

var (
	// ErrVerificationFailure is thrown when there is error while validating an email
	ErrVerificationFailure = echo.NewHTTPError(http.StatusInternalServerError, "Failed to perform email verification lookup")
	// ErrUnsupportedFormat is thrown when the requestor has defined an unsupported response format
	ErrUnsupportedFormat = echo.NewHTTPError(http.StatusBadRequest, "Unsupported format")
	// ErrInvalidCallback is thrown when the request is missing the callback queryparam
	ErrInvalidCallback = echo.NewHTTPError(http.StatusBadRequest, "Invalid callback query param provided")
)

// Lookup performs a single email verification and returns a fully
// populated lookup or an error
func (s *Service) Lookup(c echo.Context) error {
	l := s.log.WithField("handler", "Lookup")
	l.Debug("New Lookup request received")

	// Decode the email from the request
	l.Debug("Decoding the request")
	email := c.Param("email")
	l = l.WithField("email", email)

	// Parse the address passed
	l.Debug("Parsing the received email address")
	address, err := verifier.ParseAddress(email)
	if err != nil {
		l.WithError(err).Error("Failed to parse email address")
		return countAndRespond(c, http.StatusBadRequest, err)
	}

	// Check cache for a successful Lookup
	l.Debug("Checking cache for previous Lookup")
	if lookup, ok := s.lookupCache.Get(address.MD5Hash); ok {
		l.WithField("lookup", lookup).Debug("Returning Lookup found in cache")
		return countAndRespond(c, http.StatusOK, lookup)
	}

	// Performs the full email verification
	l.Debug("Performing new email verification")
	lookup, err := s.verifier.VerifyAddressTimeout(address, s.timeout)
	if err != nil {
		l.WithError(err).Error("Failed to perform verification")
		return countAndRespond(c, http.StatusInternalServerError, err)
	}
	l = l.WithField("lookup", lookup)

	// Store the lookup in cache
	l.Debug("Caching new Lookup")
	s.lookupCache.SetDefault(address.MD5Hash, lookup)

	// Returns the email validation lookup to the requestor
	l.Debug("Returning Email Lookup")
	return countAndRespond(c, http.StatusOK, lookup)
}

// countAndRespond encodes the passed response using the "format" and
// "callback" parameters on the passed echo.Context
func countAndRespond(c echo.Context, code int, res interface{}) error {
	count(res)                   // Submit metrics data
	return respond(c, code, res) // Encode the response
}

// count calls out to the various metrics APIs we have set up in order
// to submit metrics data based on the response
func count(res interface{}) {
	switch r := res.(type) {
	case *verifier.Lookup:
		if r.Deliverable {
			tinystat.CreateAction("deliverable")
		} else {
			tinystat.CreateAction("undeliverable")
		}
	case error:
		raven.CaptureError(r, nil) // Sentry metrics
		tinystat.CreateAction("error")
	}
}

// respond writes the status code and response in the desired
// format to the ResponseWriter using the passed echo.Context
func respond(c echo.Context, code int, res interface{}) error {
	// Encode the in requested format
	switch strings.ToUpper(c.Param("format")) {
	case FormatJSON:
		return c.JSON(code, res)
	case FormatJSONP:
		callback := c.QueryParam("callback")
		if callback == "" {
			return ErrInvalidCallback
		}
		return c.JSONP(code, callback, res)
	case FormatXML:
		return c.XML(code, res)
	default:
		return ErrUnsupportedFormat
	}
}
