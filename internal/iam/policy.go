// Package iam provides Identity and Access Management functionality for NebulaIO.
// It implements AWS-compatible IAM policies with extensions for advanced features.
package iam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// PolicyVersion represents the IAM policy language version.
const PolicyVersion = "2012-10-17"

// Effect represents the effect of a policy statement.
type Effect string

const (
	EffectAllow Effect = "Allow"
	EffectDeny  Effect = "Deny"
)

// Policy represents an IAM policy document.
type Policy struct {
	CreatedAt   time.Time         `json:"-"`
	UpdatedAt   time.Time         `json:"-"`
	Tags        map[string]string `json:"-"`
	ID          string            `json:"Id,omitempty"`
	Version     string            `json:"Version"`
	Name        string            `json:"-"`
	Description string            `json:"-"`
	Statements  []Statement       `json:"Statement"`
}

// Statement represents a single policy statement.
type Statement struct {
	Principal    *Principal             `json:"Principal,omitempty"`
	NotPrincipal *Principal             `json:"NotPrincipal,omitempty"`
	Condition    map[string]interface{} `json:"Condition,omitempty"`
	Sid          string                 `json:"Sid,omitempty"`
	Effect       Effect                 `json:"Effect"`
	Action       StringOrSlice          `json:"Action,omitempty"`
	NotAction    StringOrSlice          `json:"NotAction,omitempty"`
	Resource     StringOrSlice          `json:"Resource,omitempty"`
	NotResource  StringOrSlice          `json:"NotResource,omitempty"`
}

// Principal represents who the statement applies to.
type Principal struct {
	AWS       StringOrSlice `json:"AWS,omitempty"`
	Federated StringOrSlice `json:"Federated,omitempty"`
	Service   StringOrSlice `json:"Service,omitempty"`
}

// StringOrSlice handles JSON fields that can be either a string or slice of strings.
type StringOrSlice []string

// UnmarshalJSON handles unmarshaling both string and []string.
func (s *StringOrSlice) UnmarshalJSON(data []byte) error {
	var single string
	err := json.Unmarshal(data, &single)
	if err == nil {
		*s = []string{single}
		return nil
	}

	var multiple []string
	err = json.Unmarshal(data, &multiple)
	if err != nil {
		return err
	}

	*s = multiple

	return nil
}

// MarshalJSON returns a string if single element, otherwise a slice.
func (s StringOrSlice) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}

	return json.Marshal([]string(s))
}

// Contains checks if the slice contains a value.
func (s StringOrSlice) Contains(value string) bool {
	for _, v := range s {
		if v == value {
			return true
		}
	}

	return false
}

// S3Actions defines all supported S3 actions.
var S3Actions = map[string]bool{
	// Bucket operations
	"s3:CreateBucket":                     true,
	"s3:DeleteBucket":                     true,
	"s3:ListBucket":                       true,
	"s3:ListBucketVersions":               true,
	"s3:ListBucketMultipartUploads":       true,
	"s3:GetBucketLocation":                true,
	"s3:GetBucketPolicy":                  true,
	"s3:PutBucketPolicy":                  true,
	"s3:DeleteBucketPolicy":               true,
	"s3:GetBucketAcl":                     true,
	"s3:PutBucketAcl":                     true,
	"s3:GetBucketCORS":                    true,
	"s3:PutBucketCORS":                    true,
	"s3:DeleteBucketCORS":                 true,
	"s3:GetBucketVersioning":              true,
	"s3:PutBucketVersioning":              true,
	"s3:GetBucketTagging":                 true,
	"s3:PutBucketTagging":                 true,
	"s3:DeleteBucketTagging":              true,
	"s3:GetBucketLifecycleConfiguration":  true,
	"s3:PutBucketLifecycleConfiguration":  true,
	"s3:GetBucketNotification":            true,
	"s3:PutBucketNotification":            true,
	"s3:GetBucketEncryption":              true,
	"s3:PutBucketEncryption":              true,
	"s3:GetBucketObjectLockConfiguration": true,
	"s3:PutBucketObjectLockConfiguration": true,
	"s3:GetBucketReplication":             true,
	"s3:PutBucketReplication":             true,
	"s3:DeleteBucketReplication":          true,

	// Object operations
	"s3:GetObject":                  true,
	"s3:GetObjectVersion":           true,
	"s3:GetObjectAcl":               true,
	"s3:GetObjectVersionAcl":        true,
	"s3:GetObjectTagging":           true,
	"s3:GetObjectVersionTagging":    true,
	"s3:GetObjectRetention":         true,
	"s3:GetObjectLegalHold":         true,
	"s3:PutObject":                  true,
	"s3:PutObjectAcl":               true,
	"s3:PutObjectVersionAcl":        true,
	"s3:PutObjectTagging":           true,
	"s3:PutObjectVersionTagging":    true,
	"s3:PutObjectRetention":         true,
	"s3:PutObjectLegalHold":         true,
	"s3:DeleteObject":               true,
	"s3:DeleteObjectVersion":        true,
	"s3:DeleteObjectTagging":        true,
	"s3:DeleteObjectVersionTagging": true,
	"s3:RestoreObject":              true,
	"s3:AbortMultipartUpload":       true,
	"s3:ListMultipartUploadParts":   true,

	// Special operations
	"s3:*":                           true,
	"s3:GetBucketPublicAccessBlock":  true,
	"s3:PutBucketPublicAccessBlock":  true,
	"s3:GetAccountPublicAccessBlock": true,
	"s3:PutAccountPublicAccessBlock": true,
}

// AdminActions defines all supported admin actions.
var AdminActions = map[string]bool{
	"admin:*":                   true,
	"admin:CreateUser":          true,
	"admin:DeleteUser":          true,
	"admin:ListUsers":           true,
	"admin:GetUser":             true,
	"admin:UpdateUser":          true,
	"admin:CreateAccessKey":     true,
	"admin:DeleteAccessKey":     true,
	"admin:ListAccessKeys":      true,
	"admin:CreatePolicy":        true,
	"admin:DeletePolicy":        true,
	"admin:GetPolicy":           true,
	"admin:ListPolicies":        true,
	"admin:AttachUserPolicy":    true,
	"admin:DetachUserPolicy":    true,
	"admin:CreateGroup":         true,
	"admin:DeleteGroup":         true,
	"admin:AddUserToGroup":      true,
	"admin:RemoveUserFromGroup": true,
	"admin:GetClusterInfo":      true,
	"admin:ManageCluster":       true,
	"admin:ViewAuditLogs":       true,
	"admin:ManageConfig":        true,
}

// ConditionOperators defines supported condition operators.
var ConditionOperators = map[string]bool{
	// String conditions
	"StringEquals":              true,
	"StringNotEquals":           true,
	"StringEqualsIgnoreCase":    true,
	"StringNotEqualsIgnoreCase": true,
	"StringLike":                true,
	"StringNotLike":             true,

	// Numeric conditions
	"NumericEquals":            true,
	"NumericNotEquals":         true,
	"NumericLessThan":          true,
	"NumericLessThanEquals":    true,
	"NumericGreaterThan":       true,
	"NumericGreaterThanEquals": true,

	// Date conditions
	"DateEquals":            true,
	"DateNotEquals":         true,
	"DateLessThan":          true,
	"DateLessThanEquals":    true,
	"DateGreaterThan":       true,
	"DateGreaterThanEquals": true,

	// Boolean conditions
	"Bool": true,

	// Binary conditions
	"BinaryEquals": true,

	// IP address conditions
	"IpAddress":    true,
	"NotIpAddress": true,

	// ARN conditions
	"ArnEquals":    true,
	"ArnNotEquals": true,
	"ArnLike":      true,
	"ArnNotLike":   true,

	// Null condition
	"Null": true,

	// Set operators (with IfExists suffix)
	"ForAllValues:StringEquals": true,
	"ForAnyValue:StringEquals":  true,
	"ForAllValues:StringLike":   true,
	"ForAnyValue:StringLike":    true,
}

// ConditionKeys defines supported condition keys.
var ConditionKeys = map[string]bool{
	// AWS global keys
	"aws:CurrentTime":            true,
	"aws:EpochTime":              true,
	"aws:SecureTransport":        true,
	"aws:SourceIp":               true,
	"aws:UserAgent":              true,
	"aws:Referer":                true,
	"aws:PrincipalArn":           true,
	"aws:PrincipalTag/*":         true,
	"aws:RequestedRegion":        true,
	"aws:username":               true,
	"aws:userid":                 true,
	"aws:MultiFactorAuthAge":     true,
	"aws:MultiFactorAuthPresent": true,

	// S3 specific keys
	"s3:x-amz-acl":                                   true,
	"s3:x-amz-copy-source":                           true,
	"s3:x-amz-metadata-directive":                    true,
	"s3:x-amz-server-side-encryption":                true,
	"s3:x-amz-server-side-encryption-aws-kms-key-id": true,
	"s3:x-amz-storage-class":                         true,
	"s3:x-amz-grant-read":                            true,
	"s3:x-amz-grant-write":                           true,
	"s3:x-amz-grant-read-acp":                        true,
	"s3:x-amz-grant-write-acp":                       true,
	"s3:x-amz-grant-full-control":                    true,
	"s3:x-amz-content-sha256":                        true,
	"s3:prefix":                                      true,
	"s3:delimiter":                                   true,
	"s3:max-keys":                                    true,
	"s3:ExistingObjectTag/*":                         true,
	"s3:RequestObjectTag/*":                          true,
	"s3:RequestObjectTagKeys":                        true,
	"s3:VersionId":                                   true,
	"s3:LocationConstraint":                          true,
	"s3:object-lock-mode":                            true,
	"s3:object-lock-retain-until-date":               true,
	"s3:object-lock-remaining-retention-days":        true,
	"s3:object-lock-legal-hold":                      true,
}

// PolicyManager manages IAM policies.
type PolicyManager struct {
	storage        PolicyStorage
	policies       map[string]*Policy
	userPolicies   map[string][]string
	groupPolicies  map[string][]string
	bucketPolicies map[string]*Policy
	mu             sync.RWMutex
}

// PolicyStorage interface for persisting policies.
type PolicyStorage interface {
	SavePolicy(ctx context.Context, policy *Policy) error
	GetPolicy(ctx context.Context, name string) (*Policy, error)
	DeletePolicy(ctx context.Context, name string) error
	ListPolicies(ctx context.Context) ([]*Policy, error)
	SaveBucketPolicy(ctx context.Context, bucket string, policy *Policy) error
	GetBucketPolicy(ctx context.Context, bucket string) (*Policy, error)
	DeleteBucketPolicy(ctx context.Context, bucket string) error
	SaveUserPolicies(ctx context.Context, userID string, policies []string) error
	GetUserPolicies(ctx context.Context, userID string) ([]string, error)
}

// NewPolicyManager creates a new policy manager.
func NewPolicyManager(storage PolicyStorage) *PolicyManager {
	return &PolicyManager{
		policies:       make(map[string]*Policy),
		userPolicies:   make(map[string][]string),
		groupPolicies:  make(map[string][]string),
		bucketPolicies: make(map[string]*Policy),
		storage:        storage,
	}
}

// CreatePolicy creates a new IAM policy.
func (pm *PolicyManager) CreatePolicy(ctx context.Context, name, description string, document *Policy) error {
	err := pm.validatePolicy(document)
	if err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.policies[name]; exists {
		return fmt.Errorf("policy %s already exists", name)
	}

	document.Name = name
	document.Description = description
	document.ID = uuid.New().String()
	document.CreatedAt = time.Now()
	document.UpdatedAt = time.Now()

	if pm.storage != nil {
		err := pm.storage.SavePolicy(ctx, document)
		if err != nil {
			return fmt.Errorf("failed to save policy: %w", err)
		}
	}

	pm.policies[name] = document

	return nil
}

// GetPolicy retrieves a policy by name.
func (pm *PolicyManager) GetPolicy(ctx context.Context, name string) (*Policy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	policy, exists := pm.policies[name]
	if !exists {
		return nil, fmt.Errorf("policy %s not found", name)
	}

	return policy, nil
}

// DeletePolicy deletes a policy.
func (pm *PolicyManager) DeletePolicy(ctx context.Context, name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.policies[name]; !exists {
		return fmt.Errorf("policy %s not found", name)
	}

	// Check if policy is attached to any user or group
	for userID, policies := range pm.userPolicies {
		for _, p := range policies {
			if p == name {
				return fmt.Errorf("policy %s is attached to user %s", name, userID)
			}
		}
	}

	if pm.storage != nil {
		err := pm.storage.DeletePolicy(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to delete policy: %w", err)
		}
	}

	delete(pm.policies, name)

	return nil
}

// ListPolicies lists all policies.
func (pm *PolicyManager) ListPolicies(ctx context.Context) []*Policy {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	policies := make([]*Policy, 0, len(pm.policies))
	for _, policy := range pm.policies {
		policies = append(policies, policy)
	}

	return policies
}

// AttachUserPolicy attaches a policy to a user.
func (pm *PolicyManager) AttachUserPolicy(ctx context.Context, userID, policyName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.policies[policyName]; !exists {
		return fmt.Errorf("policy %s not found", policyName)
	}

	// Check if already attached
	for _, p := range pm.userPolicies[userID] {
		if p == policyName {
			return nil // Already attached
		}
	}

	pm.userPolicies[userID] = append(pm.userPolicies[userID], policyName)

	if pm.storage != nil {
		err := pm.storage.SaveUserPolicies(ctx, userID, pm.userPolicies[userID])
		if err != nil {
			return fmt.Errorf("failed to save user policies: %w", err)
		}
	}

	return nil
}

// DetachUserPolicy detaches a policy from a user.
func (pm *PolicyManager) DetachUserPolicy(ctx context.Context, userID, policyName string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	policies := pm.userPolicies[userID]
	newPolicies := make([]string, 0, len(policies))
	found := false

	for _, p := range policies {
		if p == policyName {
			found = true
			continue
		}

		newPolicies = append(newPolicies, p)
	}

	if !found {
		return fmt.Errorf("policy %s is not attached to user %s", policyName, userID)
	}

	pm.userPolicies[userID] = newPolicies

	if pm.storage != nil {
		err := pm.storage.SaveUserPolicies(ctx, userID, newPolicies)
		if err != nil {
			return fmt.Errorf("failed to save user policies: %w", err)
		}
	}

	return nil
}

// GetUserPolicies gets all policies attached to a user.
func (pm *PolicyManager) GetUserPolicies(ctx context.Context, userID string) []*Policy {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	policyNames := pm.userPolicies[userID]
	policies := make([]*Policy, 0, len(policyNames))

	for _, name := range policyNames {
		if policy, exists := pm.policies[name]; exists {
			policies = append(policies, policy)
		}
	}

	return policies
}

// SetBucketPolicy sets a bucket policy.
func (pm *PolicyManager) SetBucketPolicy(ctx context.Context, bucket string, policy *Policy) error {
	err := pm.validatePolicy(policy)
	if err != nil {
		return fmt.Errorf("invalid bucket policy: %w", err)
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.storage != nil {
		err := pm.storage.SaveBucketPolicy(ctx, bucket, policy)
		if err != nil {
			return fmt.Errorf("failed to save bucket policy: %w", err)
		}
	}

	pm.bucketPolicies[bucket] = policy

	return nil
}

// GetBucketPolicy gets a bucket policy.
func (pm *PolicyManager) GetBucketPolicy(ctx context.Context, bucket string) (*Policy, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	policy, exists := pm.bucketPolicies[bucket]
	if !exists {
		return nil, fmt.Errorf("no policy for bucket %s", bucket)
	}

	return policy, nil
}

// DeleteBucketPolicy deletes a bucket policy.
func (pm *PolicyManager) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.storage != nil {
		err := pm.storage.DeleteBucketPolicy(ctx, bucket)
		if err != nil {
			return fmt.Errorf("failed to delete bucket policy: %w", err)
		}
	}

	delete(pm.bucketPolicies, bucket)

	return nil
}

// validatePolicy validates a policy document.
func (pm *PolicyManager) validatePolicy(policy *Policy) error {
	if policy == nil {
		return errors.New("policy is nil")
	}

	if policy.Version == "" {
		policy.Version = PolicyVersion
	}

	if len(policy.Statements) == 0 {
		return errors.New("policy must have at least one statement")
	}

	for i, stmt := range policy.Statements {
		err := pm.validateStatement(stmt)
		if err != nil {
			return fmt.Errorf("statement %d: %w", i, err)
		}
	}

	return nil
}

// validateStatement validates a policy statement.
func (pm *PolicyManager) validateStatement(stmt Statement) error {
	if stmt.Effect != EffectAllow && stmt.Effect != EffectDeny {
		return fmt.Errorf("invalid effect: %s", stmt.Effect)
	}

	// Must have Action or NotAction
	if len(stmt.Action) == 0 && len(stmt.NotAction) == 0 {
		return errors.New("statement must have Action or NotAction")
	}

	// Must have Resource or NotResource
	if len(stmt.Resource) == 0 && len(stmt.NotResource) == 0 {
		return errors.New("statement must have Resource or NotResource")
	}

	// Validate actions
	actions := stmt.Action
	if len(actions) == 0 {
		actions = stmt.NotAction
	}

	for _, action := range actions {
		if !pm.isValidAction(action) {
			return fmt.Errorf("invalid action: %s", action)
		}
	}

	// Validate resources
	resources := stmt.Resource
	if len(resources) == 0 {
		resources = stmt.NotResource
	}

	for _, resource := range resources {
		if !pm.isValidResource(resource) {
			return fmt.Errorf("invalid resource: %s", resource)
		}
	}

	// Validate conditions
	if stmt.Condition != nil {
		err := pm.validateCondition(stmt.Condition)
		if err != nil {
			return fmt.Errorf("invalid condition: %w", err)
		}
	}

	return nil
}

// isValidAction checks if an action is valid.
func (pm *PolicyManager) isValidAction(action string) bool {
	// Check for wildcard
	if action == "*" {
		return true
	}

	// Check S3 actions
	if strings.HasPrefix(action, "s3:") {
		// Handle wildcards in action
		if strings.Contains(action, "*") {
			pattern := strings.ReplaceAll(action, "*", ".*")

			re, err := regexp.Compile("^" + pattern + "$")
			if err != nil {
				return false
			}

			for a := range S3Actions {
				if re.MatchString(a) {
					return true
				}
			}

			return false
		}

		return S3Actions[action]
	}

	// Check admin actions
	if strings.HasPrefix(action, "admin:") {
		if strings.Contains(action, "*") {
			pattern := strings.ReplaceAll(action, "*", ".*")

			re, err := regexp.Compile("^" + pattern + "$")
			if err != nil {
				return false
			}

			for a := range AdminActions {
				if re.MatchString(a) {
					return true
				}
			}

			return false
		}

		return AdminActions[action]
	}

	return false
}

// isValidResource checks if a resource ARN is valid.
func (pm *PolicyManager) isValidResource(resource string) bool {
	if resource == "*" {
		return true
	}

	// Basic ARN format: arn:partition:service:region:account:resource
	// For S3: arn:aws:s3:::bucket or arn:aws:s3:::bucket/key
	if !strings.HasPrefix(resource, "arn:") {
		return false
	}

	parts := strings.SplitN(resource, ":", 6)
	if len(parts) < 6 {
		return false
	}

	// Validate service
	service := parts[2]
	if service != "s3" && service != "iam" && service != "*" {
		return false
	}

	return true
}

// validateCondition validates condition block.
func (pm *PolicyManager) validateCondition(condition map[string]interface{}) error {
	for operator := range condition {
		// Check base operator or with IfExists suffix
		baseOp := strings.TrimSuffix(operator, "IfExists")
		if !ConditionOperators[baseOp] && !ConditionOperators[operator] {
			return fmt.Errorf("unsupported condition operator: %s", operator)
		}
	}

	return nil
}

// PolicyEvaluator evaluates policies for access decisions.
type PolicyEvaluator struct {
	policyManager *PolicyManager
}

// NewPolicyEvaluator creates a new policy evaluator.
func NewPolicyEvaluator(pm *PolicyManager) *PolicyEvaluator {
	return &PolicyEvaluator{policyManager: pm}
}

// EvaluationContext contains the context for policy evaluation.
type EvaluationContext struct {
	RequestTime time.Time
	Conditions  map[string]interface{}
	UserID      string
	UserArn     string
	Action      string
	Resource    string
	Bucket      string
	Key         string
	SourceIP    string
	IsSecure    bool
}

// Decision represents an access control decision.
type Decision string

const (
	DecisionAllow        Decision = "Allow"
	DecisionDeny         Decision = "Deny"
	DecisionImplicitDeny Decision = "ImplicitDeny"
)

// EvaluationResult contains the result of policy evaluation.
type EvaluationResult struct {
	Decision         Decision
	MatchedPolicy    string
	MatchedStatement string
	Reason           string
}

// Evaluate evaluates access based on policies.
func (pe *PolicyEvaluator) Evaluate(ctx context.Context, evalCtx *EvaluationContext) *EvaluationResult {
	// Step 1: Check for explicit deny in user policies
	userPolicies := pe.policyManager.GetUserPolicies(ctx, evalCtx.UserID)
	for _, policy := range userPolicies {
		for _, stmt := range policy.Statements {
			if stmt.Effect == EffectDeny && pe.statementMatches(stmt, evalCtx) {
				return &EvaluationResult{
					Decision:         DecisionDeny,
					MatchedPolicy:    policy.Name,
					MatchedStatement: stmt.Sid,
					Reason:           "Explicit deny in user policy",
				}
			}
		}
	}

	// Step 2: Check bucket policy for explicit deny
	if evalCtx.Bucket != "" {
		bucketPolicy, err := pe.policyManager.GetBucketPolicy(ctx, evalCtx.Bucket)
		if err == nil {
			for _, stmt := range bucketPolicy.Statements {
				if stmt.Effect == EffectDeny && pe.statementMatches(stmt, evalCtx) {
					return &EvaluationResult{
						Decision:         DecisionDeny,
						MatchedPolicy:    "bucket-policy",
						MatchedStatement: stmt.Sid,
						Reason:           "Explicit deny in bucket policy",
					}
				}
			}
		}
	}

	// Step 3: Check for allow in user policies
	for _, policy := range userPolicies {
		for _, stmt := range policy.Statements {
			if stmt.Effect == EffectAllow && pe.statementMatches(stmt, evalCtx) {
				return &EvaluationResult{
					Decision:         DecisionAllow,
					MatchedPolicy:    policy.Name,
					MatchedStatement: stmt.Sid,
					Reason:           "Allowed by user policy",
				}
			}
		}
	}

	// Step 4: Check bucket policy for allow
	if evalCtx.Bucket != "" {
		bucketPolicy, err := pe.policyManager.GetBucketPolicy(ctx, evalCtx.Bucket)
		if err == nil {
			for _, stmt := range bucketPolicy.Statements {
				if stmt.Effect == EffectAllow && pe.statementMatches(stmt, evalCtx) {
					return &EvaluationResult{
						Decision:         DecisionAllow,
						MatchedPolicy:    "bucket-policy",
						MatchedStatement: stmt.Sid,
						Reason:           "Allowed by bucket policy",
					}
				}
			}
		}
	}

	// Step 5: Implicit deny
	return &EvaluationResult{
		Decision: DecisionImplicitDeny,
		Reason:   "No matching allow statements",
	}
}

// statementMatches checks if a statement matches the evaluation context.
func (pe *PolicyEvaluator) statementMatches(stmt Statement, evalCtx *EvaluationContext) bool {
	// Check principal
	if !pe.principalMatches(stmt, evalCtx) {
		return false
	}

	// Check action
	if !pe.actionMatches(stmt, evalCtx.Action) {
		return false
	}

	// Check resource
	if !pe.resourceMatches(stmt, evalCtx.Resource) {
		return false
	}

	// Check conditions
	if stmt.Condition != nil && !pe.conditionMatches(stmt.Condition, evalCtx) {
		return false
	}

	return true
}

// principalMatches checks if the principal matches.
func (pe *PolicyEvaluator) principalMatches(stmt Statement, evalCtx *EvaluationContext) bool {
	// If no principal specified, it matches any authenticated user
	if stmt.Principal == nil && stmt.NotPrincipal == nil {
		return true
	}

	if stmt.Principal != nil {
		// Check AWS principals
		for _, arn := range stmt.Principal.AWS {
			if arn == "*" || arn == evalCtx.UserArn {
				return true
			}

			if pe.arnPatternMatches(arn, evalCtx.UserArn) {
				return true
			}
		}

		return false
	}

	if stmt.NotPrincipal != nil {
		// Check NotPrincipal - matches if principal is NOT in the list
		for _, arn := range stmt.NotPrincipal.AWS {
			if arn == evalCtx.UserArn || pe.arnPatternMatches(arn, evalCtx.UserArn) {
				return false
			}
		}

		return true
	}

	return false
}

// actionMatches checks if the action matches.
func (pe *PolicyEvaluator) actionMatches(stmt Statement, action string) bool {
	if len(stmt.Action) > 0 {
		for _, a := range stmt.Action {
			if a == "*" || a == action {
				return true
			}

			if pe.wildcardMatches(a, action) {
				return true
			}
		}

		return false
	}

	if len(stmt.NotAction) > 0 {
		for _, a := range stmt.NotAction {
			if a == action || pe.wildcardMatches(a, action) {
				return false
			}
		}

		return true
	}

	return false
}

// resourceMatches checks if the resource matches.
func (pe *PolicyEvaluator) resourceMatches(stmt Statement, resource string) bool {
	if len(stmt.Resource) > 0 {
		for _, r := range stmt.Resource {
			if r == "*" || r == resource {
				return true
			}

			if pe.arnPatternMatches(r, resource) {
				return true
			}
		}

		return false
	}

	if len(stmt.NotResource) > 0 {
		for _, r := range stmt.NotResource {
			if r == resource || pe.arnPatternMatches(r, resource) {
				return false
			}
		}

		return true
	}

	return false
}

// conditionMatches evaluates condition block.
func (pe *PolicyEvaluator) conditionMatches(condition map[string]interface{}, evalCtx *EvaluationContext) bool {
	for operator, keys := range condition {
		keysMap, ok := keys.(map[string]interface{})
		if !ok {
			return false
		}

		for key, expectedValue := range keysMap {
			actualValue := pe.getConditionValue(key, evalCtx)
			if !pe.evaluateCondition(operator, actualValue, expectedValue) {
				return false
			}
		}
	}

	return true
}

// getConditionValue gets the actual value for a condition key.
func (pe *PolicyEvaluator) getConditionValue(key string, evalCtx *EvaluationContext) interface{} {
	switch key {
	case "aws:SourceIp":
		return evalCtx.SourceIP
	case "aws:SecureTransport":
		return evalCtx.IsSecure
	case "aws:CurrentTime":
		return evalCtx.RequestTime.Format(time.RFC3339)
	case "aws:EpochTime":
		return evalCtx.RequestTime.Unix()
	case "aws:username":
		return evalCtx.UserID
	default:
		// Check custom conditions
		if v, ok := evalCtx.Conditions[key]; ok {
			return v
		}

		return nil
	}
}

// evaluateCondition evaluates a single condition.
func (pe *PolicyEvaluator) evaluateCondition(operator string, actual, expected interface{}) bool {
	// Handle IfExists suffix
	ifExists := strings.HasSuffix(operator, "IfExists")
	if ifExists {
		if actual == nil {
			return true
		}

		operator = strings.TrimSuffix(operator, "IfExists")
	}

	switch operator {
	case "StringEquals":
		return fmt.Sprint(actual) == fmt.Sprint(expected)
	case "StringNotEquals":
		return fmt.Sprint(actual) != fmt.Sprint(expected)
	case "StringEqualsIgnoreCase":
		return strings.EqualFold(fmt.Sprint(actual), fmt.Sprint(expected))
	case "StringLike":
		return pe.wildcardMatches(fmt.Sprint(expected), fmt.Sprint(actual))
	case "StringNotLike":
		return !pe.wildcardMatches(fmt.Sprint(expected), fmt.Sprint(actual))
	case "Bool":
		actualBool, _ := actual.(bool)
		expectedStr := fmt.Sprint(expected)

		return (actualBool && expectedStr == "true") || (!actualBool && expectedStr == "false")
	case "IpAddress":
		// Simplified IP matching
		return pe.ipMatches(fmt.Sprint(actual), fmt.Sprint(expected))
	case "NotIpAddress":
		return !pe.ipMatches(fmt.Sprint(actual), fmt.Sprint(expected))
	default:
		return false
	}
}

// wildcardMatches checks if pattern matches value with wildcards.
func (pe *PolicyEvaluator) wildcardMatches(pattern, value string) bool {
	// Convert wildcard pattern to regex
	pattern = regexp.QuoteMeta(pattern)
	pattern = strings.ReplaceAll(pattern, `\*`, ".*")
	pattern = strings.ReplaceAll(pattern, `\?`, ".")

	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return false
	}

	return re.MatchString(value)
}

// arnPatternMatches checks if ARN pattern matches value.
func (pe *PolicyEvaluator) arnPatternMatches(pattern, value string) bool {
	return pe.wildcardMatches(pattern, value)
}

// ipMatches checks if IP matches CIDR or exact IP.
func (pe *PolicyEvaluator) ipMatches(ip, cidr string) bool {
	// Parse the source IP
	sourceIP := net.ParseIP(ip)
	if sourceIP == nil {
		log.Warn().
			Str("ip", ip).
			Msg("Invalid source IP in policy condition")

		return false
	}

	// Check if cidr is a single IP (no slash)
	if !strings.Contains(cidr, "/") {
		// Exact IP match
		targetIP := net.ParseIP(cidr)
		if targetIP == nil {
			log.Warn().
				Str("cidr", cidr).
				Msg("Invalid target IP in policy condition")

			return false
		}

		return sourceIP.Equal(targetIP)
	}

	// Parse as CIDR
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		// Invalid CIDR, fall back to prefix match for backwards compatibility
		log.Warn().
			Str("cidr", cidr).
			Err(err).
			Msg("Invalid CIDR notation, falling back to prefix matching")

		return strings.HasPrefix(ip, strings.TrimSuffix(cidr, "/*"))
	}

	// Check if the source IP is within the CIDR network
	return network.Contains(sourceIP)
}

// Predefined policies.
var (
	// ReadOnlyPolicy allows read-only access to all buckets.
	ReadOnlyPolicy = &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Sid:    "ReadOnlyAccess",
				Effect: EffectAllow,
				Action: StringOrSlice{
					"s3:GetObject",
					"s3:GetObjectVersion",
					"s3:GetObjectAcl",
					"s3:GetObjectTagging",
					"s3:ListBucket",
					"s3:ListBucketVersions",
					"s3:GetBucketLocation",
				},
				Resource: StringOrSlice{"arn:aws:s3:::*", "arn:aws:s3:::*/*"},
			},
		},
	}

	// ReadWritePolicy allows read and write access to all buckets.
	ReadWritePolicy = &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Sid:    "ReadWriteAccess",
				Effect: EffectAllow,
				Action: StringOrSlice{
					"s3:GetObject",
					"s3:GetObjectVersion",
					"s3:PutObject",
					"s3:DeleteObject",
					"s3:ListBucket",
					"s3:ListBucketVersions",
					"s3:GetBucketLocation",
				},
				Resource: StringOrSlice{"arn:aws:s3:::*", "arn:aws:s3:::*/*"},
			},
		},
	}

	// AdminPolicy allows full admin access.
	AdminPolicy = &Policy{
		Version: PolicyVersion,
		Statements: []Statement{
			{
				Sid:      "AdminAccess",
				Effect:   EffectAllow,
				Action:   StringOrSlice{"s3:*", "admin:*"},
				Resource: StringOrSlice{"*"},
			},
		},
	}
)
