package credsource

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type awsSource struct{}

const (
	awsRequestTimeout = 15 * time.Second
	awsAlgorithm      = "AWS4-HMAC-SHA256"
	awsService        = "secretsmanager"
)

var newAWSClient = func() *http.Client {
	return &http.Client{Timeout: awsRequestTimeout}
}

func (awsSource) Resolve(ctx context.Context, ref Reference) (Value, error) {
	accessKey, secretKey, sessionToken, region, err := awsAuth()
	if err != nil {
		return Value{}, err
	}
	host := fmt.Sprintf("%s.%s.amazonaws.com", awsService, region)
	reqURL := "https://" + host + "/"
	if endpoint := strings.TrimSpace(os.Getenv("AWS_ENDPOINT_URL")); endpoint != "" {
		u, err := url.Parse(strings.TrimRight(endpoint, "/") + "/")
		if err != nil || u.Host == "" {
			return Value{}, fmt.Errorf("invalid AWS_ENDPOINT_URL")
		}
		host = u.Host
		reqURL = u.String()
	}
	body, err := json.Marshal(map[string]string{"SecretId": ref.Name})
	if err != nil {
		return Value{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return Value{}, err
	}
	signAWSRequest(req, host, region, accessKey, secretKey, sessionToken, body)

	resp, err := newAWSClient().Do(req)
	if err != nil {
		return Value{}, fmt.Errorf("aws secrets manager request for %s: %w", ref.Name, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return Value{}, awsError(ref.Name, resp.StatusCode, respBody)
	}
	var parsed struct {
		SecretString string `json:"SecretString"`
		SecretBinary string `json:"SecretBinary"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Value{}, fmt.Errorf("aws secrets manager %s: malformed response", ref.Name)
	}
	if parsed.SecretString != "" {
		return awsFieldBytes(ref.Name, ref.Field, parsed.SecretString)
	}
	if parsed.SecretBinary != "" {
		raw, err := base64.StdEncoding.DecodeString(parsed.SecretBinary)
		if err != nil {
			return Value{}, fmt.Errorf("aws secrets manager %s: malformed SecretBinary", ref.Name)
		}
		return Value{Bytes: raw}, nil
	}
	return Value{}, fmt.Errorf("%w: aws secrets manager %s", ErrNotFound, ref.Name)
}

func (awsSource) Close() error { return nil }

func awsAuth() (accessKey, secretKey, sessionToken, region string, err error) {
	accessKey = strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID"))
	secretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessionToken = strings.TrimSpace(os.Getenv("AWS_SESSION_TOKEN"))
	region = strings.TrimSpace(os.Getenv("AWS_REGION"))
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	if accessKey == "" || strings.TrimSpace(secretKey) == "" {
		var fileRegion string
		accessKey, secretKey, sessionToken, fileRegion = awsSharedFileAuth()
		if region == "" {
			region = fileRegion
		}
	} else if region == "" {
		_, _, _, region = awsSharedFileAuth()
	}
	if accessKey == "" || strings.TrimSpace(secretKey) == "" {
		return "", "", "", "", fmt.Errorf("%w: aws source needs AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, or ~/.aws/credentials", ErrLocked)
	}
	if region == "" {
		return "", "", "", "", fmt.Errorf("aws source needs AWS_REGION or region in ~/.aws/config")
	}
	return accessKey, secretKey, sessionToken, region, nil
}

func awsSharedFileAuth() (accessKey, secretKey, sessionToken, region string) {
	profile := strings.TrimSpace(os.Getenv("AWS_PROFILE"))
	if profile == "" {
		profile = "default"
	}
	home, _ := os.UserHomeDir()
	credPath := strings.TrimSpace(os.Getenv("AWS_SHARED_CREDENTIALS_FILE"))
	if credPath == "" && home != "" {
		credPath = filepath.Join(home, ".aws", "credentials")
	}
	configPath := strings.TrimSpace(os.Getenv("AWS_CONFIG_FILE"))
	if configPath == "" && home != "" {
		configPath = filepath.Join(home, ".aws", "config")
	}
	if creds := parseAWSINIFile(credPath)[profile]; creds != nil {
		accessKey = strings.TrimSpace(creds["aws_access_key_id"])
		secretKey = creds["aws_secret_access_key"]
		sessionToken = strings.TrimSpace(creds["aws_session_token"])
	}
	cfgSection := "default"
	if profile != "default" {
		cfgSection = "profile " + profile
	}
	if cfg := parseAWSINIFile(configPath)[cfgSection]; cfg != nil {
		region = strings.TrimSpace(cfg["region"])
	}
	return accessKey, secretKey, sessionToken, region
}

func parseAWSINIFile(path string) map[string]map[string]string {
	if path == "" {
		return map[string]map[string]string{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]map[string]string{}
	}
	return parseAWSINI(string(data))
}

func parseAWSINI(data string) map[string]map[string]string {
	out := map[string]map[string]string{}
	section := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if out[section] == nil {
				out[section] = map[string]string{}
			}
			continue
		}
		if section == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		out[section][k] = v
	}
	return out
}

func awsFieldBytes(name, field, secretString string) (Value, error) {
	if field == "" {
		return Value{Bytes: []byte(secretString)}, nil
	}
	var obj map[string]any
	if err := decodeJSONUseNumber([]byte(secretString), &obj); err != nil {
		return Value{}, fmt.Errorf("aws secrets manager %s: SecretString is not a JSON object, cannot select field %q", name, field)
	}
	raw, ok := obj[field]
	if !ok {
		return Value{}, fmt.Errorf("%w: aws secrets manager %s has no field %q", ErrNotFound, name, field)
	}
	return Value{Bytes: vaultFieldBytes(raw)}, nil
}

func awsError(name string, status int, body []byte) error {
	var parsed struct {
		Type    string `json:"__type"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	kind := parsed.Type
	if i := strings.LastIndex(kind, "#"); i >= 0 {
		kind = kind[i+1:]
	}
	switch {
	case kind == "ResourceNotFoundException" || status == http.StatusNotFound:
		return fmt.Errorf("%w: aws secrets manager %s", ErrNotFound, name)
	case kind == "AccessDeniedException" || status == http.StatusForbidden:
		return fmt.Errorf("aws secrets manager %s: access denied (check the IAM policy for secretsmanager:GetSecretValue)", name)
	case kind == "UnrecognizedClientException":
		return fmt.Errorf("aws secrets manager %s: invalid credentials (check AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY)", name)
	default:
		msg := strings.TrimSpace(parsed.Message)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("aws secrets manager %s: %d %s", name, status, msg)
	}
}

func signAWSRequest(req *http.Request, host, region, accessKey, secretKey, sessionToken string, body []byte) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	payloadHash := hex.EncodeToString(sum256(body))

	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Target", awsService+".GetSecretValue")
	if sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
	}
	req.Host = host

	type hdr struct{ name, value string }
	headers := []hdr{
		{"content-type", req.Header.Get("Content-Type")},
		{"host", host},
		{"x-amz-date", amzDate},
	}
	if sessionToken != "" {
		headers = append(headers, hdr{"x-amz-security-token", sessionToken})
	}
	headers = append(headers, hdr{"x-amz-target", req.Header.Get("X-Amz-Target")})

	var canonHeaders strings.Builder
	var signedNames strings.Builder
	for i, h := range headers {
		canonHeaders.WriteString(h.name)
		canonHeaders.WriteString(":")
		canonHeaders.WriteString(strings.TrimSpace(h.value))
		canonHeaders.WriteString("\n")
		if i > 0 {
			signedNames.WriteString(";")
		}
		signedNames.WriteString(h.name)
	}
	signedHeaders := signedNames.String()

	canonicalRequest := strings.Join([]string{
		"POST",
		"/",
		"",
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := strings.Join([]string{dateStamp, region, awsService, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		awsAlgorithm,
		amzDate,
		credentialScope,
		hex.EncodeToString(sum256([]byte(canonicalRequest))),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(
		signingKey(secretKey, dateStamp, region, awsService),
		[]byte(stringToSign),
	))
	req.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		awsAlgorithm, accessKey, credentialScope, signedHeaders, signature,
	))
}

func signingKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sum256(data []byte) []byte {
	s := sha256.Sum256(data)
	return s[:]
}
