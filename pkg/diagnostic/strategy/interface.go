package strategy

import "github.com/nuts-project/nuts/pkg/diagnostic"

// DiagnosticStrategy is the interface for all diagnostic strategies
type DiagnosticStrategy interface {
	// Name returns the name of the strategy
	Name() string

	// Analyze performs diagnostic analysis
	Analyze(audit *diagnostic.Audit) (*diagnostic.DiagnosisResult, error)
}

// BuiltInDiagnosticStrategy implements rule-based diagnostic strategy
type BuiltInDiagnosticStrategy struct {
	rulesPath string
}

// NewBuiltInDiagnosticStrategy creates a new built-in diagnostic strategy
func NewBuiltInDiagnosticStrategy(rulesPath string) *BuiltInDiagnosticStrategy {
	return &BuiltInDiagnosticStrategy{
		rulesPath: rulesPath,
	}
}

// Name returns the name of the strategy
func (s *BuiltInDiagnosticStrategy) Name() string {
	return "builtin"
}

// Analyze performs diagnostic analysis using built-in rules
func (s *BuiltInDiagnosticStrategy) Analyze(audit *diagnostic.Audit) (*diagnostic.DiagnosisResult, error) {
	// Implementation to be added
	return &diagnostic.DiagnosisResult{}, nil
}

// AIDiagnosticStrategy implements AI-based diagnostic strategy
type AIDiagnosticStrategy struct {
	aiClient AIClient
}

// NewAIDiagnosticStrategy creates a new AI diagnostic strategy
func NewAIDiagnosticStrategy(aiClient AIClient) *AIDiagnosticStrategy {
	return &AIDiagnosticStrategy{
		aiClient: aiClient,
	}
}

// Name returns the name of the strategy
func (s *AIDiagnosticStrategy) Name() string {
	return "ai"
}

// Analyze performs diagnostic analysis using AI
func (s *AIDiagnosticStrategy) Analyze(audit *diagnostic.Audit) (*diagnostic.DiagnosisResult, error) {
	// Implementation to be added
	return &diagnostic.DiagnosisResult{}, nil
}

// AIClient is the interface for AI client
type AIClient interface {
	// Analyze sends audit data to AI and returns diagnosis
	Analyze(audit *diagnostic.Audit) (*diagnostic.DiagnosisResult, error)
}
