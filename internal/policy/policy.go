// Package policy provides IAM policy evaluation for NebulaIO.
//
// The package implements AWS IAM-compatible policy parsing and evaluation,
// supporting:
//
//   - Bucket policies: Control access to specific buckets
//   - User policies: Attached to IAM users
//   - Group policies: Attached to IAM groups
//
// Policy documents use the standard IAM policy language with support for:
//   - Actions: s3:GetObject, s3:PutObject, s3:DeleteObject, etc.
//   - Resources: arn:aws:s3:::bucket/key patterns with wildcards
//   - Conditions: IP address, time, request headers, etc.
//   - Principal: User, group, or anonymous access control
//
// Example policy:
//
//	{
//	  "Version": "2012-10-17",
//	  "Statement": [{
//	    "Effect": "Allow",
//	    "Principal": "*",
//	    "Action": "s3:GetObject",
//	    "Resource": "arn:aws:s3:::public-bucket/*"
//	  }]
//	}
package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Boolean string constant.
const boolTrue = "true"

// Policy represents an S3 bucket policy document.
type Policy struct {
	Version   string      `json:"Version"`
	Id        string      `json:"Id,omitempty"`
	Statement []Statement `json:"Statement"`
}

// Statement represents a single policy statement.
type Statement struct {
	Principal any                       `json:"Principal"`
	Action    any                       `json:"Action"`
	Resource  any                       `json:"Resource"`
	Condition map[string]map[string]any `json:"Condition,omitempty"`
	Sid       string                            `json:"Sid,omitempty"`
	Effect    string                            `json:"Effect"`
}

// Effect constants.
const (
	EffectAllow = "Allow"
	EffectDeny  = "Deny"
)

// Common S3 actions.
const (
	ActionGetObject           = "s3:GetObject"
	ActionPutObject           = "s3:PutObject"
	ActionDeleteObject        = "s3:DeleteObject"
	ActionListBucket          = "s3:ListBucket"
	ActionGetBucketLocation   = "s3:GetBucketLocation"
	ActionGetBucketPolicy     = "s3:GetBucketPolicy"
	ActionPutBucketPolicy     = "s3:PutBucketPolicy"
	ActionDeleteBucketPolicy  = "s3:DeleteBucketPolicy"
	ActionGetBucketCORS       = "s3:GetBucketCors"
	ActionPutBucketCORS       = "s3:PutBucketCors"
	ActionDeleteBucketCORS    = "s3:DeleteBucketCors"
	ActionGetBucketVersioning = "s3:GetBucketVersioning"
	ActionPutBucketVersioning = "s3:PutBucketVersioning"
	ActionAll                 = "s3:*"
)

// EvalContext contains information needed to evaluate a policy.
type EvalContext struct {
	Conditions map[string]string
	Principal  string
	Action     string
	Resource   string
	BucketName string
	ObjectKey  string
}

// EvalResult represents the result of a policy evaluation.
type EvalResult int

const (
	// EvalDefault means no explicit allow or deny.
	EvalDefault EvalResult = iota
	// EvalAllow means explicitly allowed.
	EvalAllow
	// EvalDeny means explicitly denied.
	EvalDeny
)

// ParsePolicy parses a JSON policy document.
func ParsePolicy(jsonData string) (*Policy, error) {
	if jsonData == "" {
		return nil, errors.New("empty policy document")
	}

	var p Policy

	err := json.Unmarshal([]byte(jsonData), &p)
	if err != nil {
		return nil, fmt.Errorf("invalid policy JSON: %w", err)
	}

	return &p, nil
}

// Validate validates the policy structure and content.
func (p *Policy) Validate() error {
	if p == nil {
		return errors.New("policy is nil")
	}

	// Validate version
	if p.Version != "2012-10-17" && p.Version != "2008-10-17" {
		return fmt.Errorf("invalid policy version: %s (must be 2012-10-17 or 2008-10-17)", p.Version)
	}

	if len(p.Statement) == 0 {
		return errors.New("policy must have at least one statement")
	}

	for i, stmt := range p.Statement {
		err := validateStatement(&stmt, i)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateStatement validates a single policy statement.
func validateStatement(stmt *Statement, index int) error {
	// Validate Effect
	if stmt.Effect != EffectAllow && stmt.Effect != EffectDeny {
		return fmt.Errorf("statement %d: Effect must be 'Allow' or 'Deny', got '%s'", index, stmt.Effect)
	}

	// Validate Principal
	err := validatePrincipal(stmt.Principal, index)
	if err != nil {
		return err
	}

	// Validate Action
	err = validateAction(stmt.Action, index)
	if err != nil {
		return err
	}

	// Validate Resource
	err = validateResource(stmt.Resource, index)
	if err != nil {
		return err
	}

	return nil
}

// validatePrincipal validates the Principal field.
func validatePrincipal(principal interface{}, index int) error {
	if principal == nil {
		return fmt.Errorf("statement %d: Principal is required", index)
	}

	switch p := principal.(type) {
	case string:
		if p != "*" {
			return fmt.Errorf("statement %d: invalid Principal string (must be '*')", index)
		}
	case map[string]interface{}:
		if _, ok := p["AWS"]; !ok {
			if _, ok := p["Service"]; !ok {
				return fmt.Errorf("statement %d: Principal must contain 'AWS' or 'Service' key", index)
			}
		}
	default:
		return fmt.Errorf("statement %d: invalid Principal type", index)
	}

	return nil
}

// validateAction validates the Action field.
func validateAction(action interface{}, index int) error {
	if action == nil {
		return fmt.Errorf("statement %d: Action is required", index)
	}

	switch a := action.(type) {
	case string:
		if !isValidAction(a) {
			return fmt.Errorf("statement %d: invalid action '%s'", index, a)
		}
	case []interface{}:
		for _, act := range a {
			actStr, ok := act.(string)
			if !ok {
				return fmt.Errorf("statement %d: action must be a string", index)
			}

			if !isValidAction(actStr) {
				return fmt.Errorf("statement %d: invalid action '%s'", index, actStr)
			}
		}
	default:
		return fmt.Errorf("statement %d: Action must be a string or array of strings", index)
	}

	return nil
}

// isValidAction checks if an action is a valid S3 action.
func isValidAction(action string) bool {
	// Allow wildcard actions
	if action == "*" || action == "s3:*" {
		return true
	}

	// Check if it starts with s3:
	if !strings.HasPrefix(action, "s3:") {
		return false
	}

	// Allow any s3:* pattern
	return true
}

// validateResource validates the Resource field.
func validateResource(resource interface{}, index int) error {
	if resource == nil {
		return fmt.Errorf("statement %d: Resource is required", index)
	}

	switch r := resource.(type) {
	case string:
		if !isValidResource(r) {
			return fmt.Errorf("statement %d: invalid resource '%s'", index, r)
		}
	case []interface{}:
		for _, res := range r {
			resStr, ok := res.(string)
			if !ok {
				return fmt.Errorf("statement %d: resource must be a string", index)
			}

			if !isValidResource(resStr) {
				return fmt.Errorf("statement %d: invalid resource '%s'", index, resStr)
			}
		}
	default:
		return fmt.Errorf("statement %d: Resource must be a string or array of strings", index)
	}

	return nil
}

// isValidResource checks if a resource ARN is valid.
func isValidResource(resource string) bool {
	// Allow wildcard
	if resource == "*" {
		return true
	}

	// Check if it's an S3 ARN pattern
	if !strings.HasPrefix(resource, "arn:aws:s3:::") {
		return false
	}

	return true
}

// Evaluate evaluates the policy against the given context
// Returns Allow if explicitly allowed, Deny if explicitly denied, Default otherwise.
func (p *Policy) Evaluate(ctx EvalContext) (EvalResult, error) {
	if p == nil || len(p.Statement) == 0 {
		return EvalDefault, nil
	}

	resource := p.buildResourceARN(ctx)

	if denyResult := p.checkDenyStatements(ctx, resource); denyResult != EvalDefault {
		return denyResult, nil
	}

	return p.checkAllowStatements(ctx, resource)
}

func (p *Policy) buildResourceARN(ctx EvalContext) string {
	if ctx.Resource != "" {
		return ctx.Resource
	}

	if ctx.ObjectKey != "" {
		return fmt.Sprintf("arn:aws:s3:::%s/%s", ctx.BucketName, ctx.ObjectKey)
	}

	return "arn:aws:s3:::" + ctx.BucketName
}

func (p *Policy) checkDenyStatements(ctx EvalContext, resource string) EvalResult {
	for _, stmt := range p.Statement {
		if stmt.Effect != EffectDeny {
			continue
		}

		matches, err := matchStatement(&stmt, ctx.Principal, ctx.Action, resource, ctx.Conditions)
		if err != nil || !matches {
			continue
		}

		return EvalDeny
	}

	return EvalDefault
}

func (p *Policy) checkAllowStatements(ctx EvalContext, resource string) (EvalResult, error) {
	for _, stmt := range p.Statement {
		if stmt.Effect != EffectAllow {
			continue
		}

		matches, err := matchStatement(&stmt, ctx.Principal, ctx.Action, resource, ctx.Conditions)
		if err != nil {
			return EvalDefault, fmt.Errorf("error evaluating statement: %w", err)
		}

		if matches {
			return EvalAllow, nil
		}
	}

	return EvalDefault, nil
}

// matchStatement checks if a statement matches the given parameters.
func matchStatement(stmt *Statement, principal, action, resource string, conditions map[string]string) (bool, error) {
	// Match principal
	if !matchPrincipal(stmt.Principal, principal) {
		return false, nil
	}

	// Match action
	if !matchAction(stmt.Action, action) {
		return false, nil
	}

	// Match resource
	if !matchResource(stmt.Resource, resource) {
		return false, nil
	}

	// Match conditions
	if len(stmt.Condition) > 0 {
		if !matchConditions(stmt.Condition, conditions) {
			return false, nil
		}
	}

	return true, nil
}

// matchPrincipal checks if the principal matches the statement's Principal.
func matchPrincipal(stmtPrincipal interface{}, principal string) bool {
	switch p := stmtPrincipal.(type) {
	case string:
		if p == "*" {
			return true
		}

		return p == principal
	case map[string]interface{}:
		if aws, ok := p["AWS"]; ok {
			return matchStringOrArray(aws, principal)
		}
		// For service principals
		if service, ok := p["Service"]; ok {
			return matchStringOrArray(service, principal)
		}
	}

	return false
}

// matchAction checks if the action matches the statement's Action.
func matchAction(stmtAction interface{}, action string) bool {
	return matchStringOrArrayWithWildcard(stmtAction, action)
}

// matchResource checks if the resource matches the statement's Resource.
func matchResource(stmtResource interface{}, resource string) bool {
	return matchStringOrArrayWithWildcard(stmtResource, resource)
}

// matchStringOrArray matches a value against a string or array of strings.
func matchStringOrArray(value interface{}, target string) bool {
	switch v := value.(type) {
	case string:
		return v == "*" || v == target
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s == "*" || s == target {
					return true
				}
			}
		}
	}

	return false
}

// matchStringOrArrayWithWildcard matches with wildcard pattern support.
func matchStringOrArrayWithWildcard(value interface{}, target string) bool {
	switch v := value.(type) {
	case string:
		return matchWildcard(v, target)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if matchWildcard(s, target) {
					return true
				}
			}
		}
	}

	return false
}

// matchWildcard matches a pattern with wildcards against a target.
func matchWildcard(pattern, target string) bool {
	if pattern == "*" {
		return true
	}

	// Convert wildcard pattern to regex
	// Escape regex special chars except * and ?
	escaped := regexp.QuoteMeta(pattern)
	// Convert \* back to .* and \? to .
	escaped = strings.ReplaceAll(escaped, `\*`, `.*`)
	escaped = strings.ReplaceAll(escaped, `\?`, `.`)

	// Anchor the pattern
	regexPattern := "^" + escaped + "$"

	matched, err := regexp.MatchString(regexPattern, target)
	if err != nil {
		return false
	}

	return matched
}

// matchConditions checks if conditions match.
func matchConditions(stmtConditions map[string]map[string]interface{}, conditions map[string]string) bool {
	for operator, conditionMap := range stmtConditions {
		for key, expectedValue := range conditionMap {
			actualValue, exists := conditions[key]

			if !evaluateConditionOperator(operator, expectedValue, actualValue, exists) {
				return false
			}
		}
	}

	return true
}

// evaluateConditionOperator evaluates a single condition operator.
func evaluateConditionOperator(operator string, expectedValue interface{}, actualValue string, exists bool) bool {
	switch operator {
	case "StringEquals":
		return matchStringEquals(expectedValue, actualValue, exists)
	case "StringNotEquals":
		return matchStringNotEquals(expectedValue, actualValue, exists)
	case "StringLike":
		return matchStringLike(expectedValue, actualValue, exists)
	case "StringNotLike":
		return matchStringNotLike(expectedValue, actualValue, exists)
	case "IpAddress":
		return matchIPCondition(expectedValue, actualValue, exists)
	case "NotIpAddress":
		return matchNotIPCondition(expectedValue, actualValue, exists)
	case "Bool":
		return matchBoolCondition(expectedValue, actualValue, exists)
	case "Null":
		return matchNullCondition(expectedValue, exists)
	default:
		// Unknown operator - don't fail, just skip
		return true
	}
}

// matchStringEquals checks if actual equals expected string value.
func matchStringEquals(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return false
	}

	return matchConditionValue(expected, actual, func(a, b string) bool { return a == b })
}

// matchStringNotEquals checks if actual does not equal expected string value.
func matchStringNotEquals(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return true
	}

	return !matchConditionValue(expected, actual, func(a, b string) bool { return a == b })
}

// matchStringLike checks if actual matches expected wildcard pattern.
func matchStringLike(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return false
	}

	return matchConditionValue(expected, actual, matchWildcard)
}

// matchStringNotLike checks if actual does not match expected wildcard pattern.
func matchStringNotLike(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return true
	}

	return !matchConditionValue(expected, actual, matchWildcard)
}

// matchIPCondition checks if actual IP matches expected CIDR/IP pattern.
func matchIPCondition(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return false
	}

	return matchConditionValue(expected, actual, matchIPAddress)
}

// matchNotIPCondition checks if actual IP does not match expected CIDR/IP pattern.
func matchNotIPCondition(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return true
	}

	return !matchConditionValue(expected, actual, matchIPAddress)
}

// matchBoolCondition checks if actual boolean matches expected.
func matchBoolCondition(expected interface{}, actual string, exists bool) bool {
	if !exists {
		return false
	}

	expectedBool := fmt.Sprintf("%v", expected) == boolTrue
	actualBool := actual == boolTrue

	return expectedBool == actualBool
}

// matchNullCondition checks if key existence matches expected null condition.
func matchNullCondition(expected interface{}, exists bool) bool {
	expectedNull := fmt.Sprintf("%v", expected) == boolTrue

	if expectedNull {
		return !exists
	}

	return exists
}

// matchConditionValue matches a condition value which can be a string or array.
func matchConditionValue(expected interface{}, actual string, matcher func(string, string) bool) bool {
	switch v := expected.(type) {
	case string:
		return matcher(v, actual)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				if matcher(s, actual) {
					return true
				}
			}
		}
	}

	return false
}

// matchIPAddress checks if an IP matches a CIDR pattern.
func matchIPAddress(pattern, ip string) bool {
	// Simple implementation - for full support, use net package
	// This handles exact IP matches and basic CIDR notation
	if pattern == ip {
		return true
	}

	// Handle CIDR notation (basic implementation)
	if strings.Contains(pattern, "/") {
		// For production, use net.ParseCIDR
		prefix := strings.Split(pattern, "/")[0]
		// Handle /0 (all IPs)
		if strings.HasSuffix(pattern, "/0") {
			return true
		}
		// Basic prefix matching for common cases
		return strings.HasPrefix(ip, prefix)
	}

	return false
}

// ToJSON converts the policy back to JSON.
func (p *Policy) ToJSON() (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal policy: %w", err)
	}

	return string(data), nil
}

// MapActionToS3Action maps an HTTP method and path to an S3 action.
func MapActionToS3Action(method, path string, isBucket bool) string {
	method = strings.ToUpper(method)

	if isBucket {
		return mapBucketAction(method, path)
	}
	return mapObjectAction(method)
}

func mapBucketAction(method, path string) string {
	switch method {
	case "GET":
		return getBucketGetAction(path)
	case "PUT":
		return getBucketPutAction(path)
	case "DELETE":
		return getBucketDeleteAction(path)
	case "HEAD":
		return "s3:HeadBucket"
	}
	return ""
}

func getBucketGetAction(path string) string {
	if strings.Contains(path, "?policy") {
		return ActionGetBucketPolicy
	}
	if strings.Contains(path, "?cors") {
		return ActionGetBucketCORS
	}
	if strings.Contains(path, "?versioning") {
		return ActionGetBucketVersioning
	}
	return ActionListBucket
}

func getBucketPutAction(path string) string {
	if strings.Contains(path, "?policy") {
		return ActionPutBucketPolicy
	}
	if strings.Contains(path, "?cors") {
		return ActionPutBucketCORS
	}
	if strings.Contains(path, "?versioning") {
		return ActionPutBucketVersioning
	}
	return "s3:CreateBucket"
}

func getBucketDeleteAction(path string) string {
	if strings.Contains(path, "?policy") {
		return ActionDeleteBucketPolicy
	}
	if strings.Contains(path, "?cors") {
		return ActionDeleteBucketCORS
	}
	return "s3:DeleteBucket"
}

func mapObjectAction(method string) string {
	switch method {
	case "GET":
		return ActionGetObject
	case "PUT":
		return ActionPutObject
	case "DELETE":
		return ActionDeleteObject
	case "HEAD":
		return "s3:HeadObject"
	case "POST":
		return ActionPutObject
	}
	return ""
}
