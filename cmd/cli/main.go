package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"
)

const (
	defaultAPIURL = "http://localhost:8080"
)

var apiURL string

func init() {
	if url := os.Getenv("NUTS_API_URL"); url != "" {
		apiURL = url
	} else {
		apiURL = defaultAPIURL
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "nuts",
		Short: "Nuts CLI - Policy management for container monitoring",
		Long:  `Nuts CLI is a command-line tool for managing monitoring policies in the Nuts system.`,
	}

	// Add version command
	rootCmd.AddCommand(versionCmd())

	// Add policy command
	rootCmd.AddCommand(policyCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Nuts CLI v0.2.0")
		},
	}
}

func policyCmd() *cobra.Command {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage policies",
		Long:  `Manage monitoring policies including create, update, delete, get, and list operations.`,
	}

	policyCmd.AddCommand(createPolicyCmd())
	policyCmd.AddCommand(updatePolicyCmd())
	policyCmd.AddCommand(deletePolicyCmd())
	policyCmd.AddCommand(getPolicyCmd())
	policyCmd.AddCommand(listPoliciesCmd())

	return policyCmd
}

// Policy represents a policy
type Policy struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Metrics   map[string][]string `json:"metrics"` // key: category, value: list of script names
	Duration  int64               `json:"duration"`
	Rule      string              `json:"rule"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// PolicyFile represents the policy JSON file (without rule field)
type PolicyFile struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Metrics  map[string][]string `json:"metrics"` // key: category, value: list of script names
	Duration int64               `json:"duration"`
}

// RuleFile represents the rule YAML file
type RuleFile struct {
	Rule      string   `yaml:"rule"`
	Desc      string   `yaml:"desc"`
	Condition string   `yaml:"condition"`
	Output    string   `yaml:"output"`
	Priority  string   `yaml:"priority"`
	Tags      []string `yaml:"tags"`
	Enabled   *bool    `yaml:"enabled"`
}

// CreatePolicyRequest represents the request to create a policy
type CreatePolicyRequest struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Metrics  map[string][]string `json:"metrics"` // key: category, value: list of script names
	Duration int64               `json:"duration"`
	Rule     string              `json:"rule"`
}

// UpdatePolicyRequest represents the request to update a policy
type UpdatePolicyRequest struct {
	Name     *string             `json:"name,omitempty"`
	Metrics  map[string][]string `json:"metrics,omitempty"` // key: category, value: list of script names
	Duration *int64              `json:"duration,omitempty"`
	Rule     *string             `json:"rule,omitempty"`
}

func createPolicyCmd() *cobra.Command {
	var policyFile, ruleFile string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new policy",
		Long:  `Create a new monitoring policy from separate policy JSON and rule YAML files.`,
		Example: `  nuts policy create --policy policy.json --rule rule.yaml
		
		Example policy.json:
		{
		  "id": "policy-1",
		  "name": "Monitor privileged containers",
		  "metrics": {
		    "process": ["process.bt"],
		    "network": ["network.bt"]
		  },
		  "duration": 60
		}
		
		Example rule.yaml:
		- rule: container.privileged = true
		  desc: Detect privileged containers
		  condition: container.privileged = true
		  output: "Privileged container detected: {{container.name}}"
		  priority: high
		  tags: ["security", "privileged"]
		  enabled: true`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyFile == "" {
				return fmt.Errorf("--policy flag is required")
			}
			if ruleFile == "" {
				return fmt.Errorf("--rule flag is required")
			}

			// Read and parse policy file
			policyData, err := os.ReadFile(policyFile)
			if err != nil {
				return fmt.Errorf("error reading policy file: %w", err)
			}

			var pf PolicyFile
			if err := json.Unmarshal(policyData, &pf); err != nil {
				return fmt.Errorf("error parsing policy file: %w", err)
			}

			// Read rule file (send full YAML content)
			ruleData, err := os.ReadFile(ruleFile)
			if err != nil {
				return fmt.Errorf("error reading rule file: %w", err)
			}

			// Validate that rule file has content
			if len(ruleData) == 0 {
				return fmt.Errorf("rule file is empty")
			}

			// Merge policy and rule into request
			req := CreatePolicyRequest{
				ID:       pf.ID,
				Name:     pf.Name,
				Metrics:  pf.Metrics,
				Duration: pf.Duration,
				Rule:     string(ruleData), // Send full YAML content
			}

			// Send request
			body, err := json.Marshal(req)
			if err != nil {
				return fmt.Errorf("error marshaling request: %w", err)
			}

			resp, err := http.Post(apiURL+"/api/v1/policies", "application/json", bytes.NewBuffer(body))
			if err != nil {
				return fmt.Errorf("error creating policy: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				return printErrorResponse(resp)
			}

			var policy Policy
			if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
				return fmt.Errorf("error decoding response: %w", err)
			}

			fmt.Println("Policy created successfully:")
			printPolicy(&policy)
			return nil
		},
	}

	cmd.Flags().StringVar(&policyFile, "policy", "", "Path to policy JSON file (required)")
	cmd.Flags().StringVar(&ruleFile, "rule", "", "Path to rule YAML file (required)")
	cmd.MarkFlagRequired("policy")
	cmd.MarkFlagRequired("rule")

	return cmd
}

func updatePolicyCmd() *cobra.Command {
	var policyFile, ruleFile string

	cmd := &cobra.Command{
		Use:   "update <policy-id>",
		Short: "Update an existing policy",
		Long:  `Update an existing monitoring policy from separate policy JSON and rule YAML files.`,
		Example: `  nuts policy update policy-1 --policy policy.json --rule rule.yaml
  
  Example policy.json:
  {
    "name": "Monitor privileged containers (updated)",
    "metrics": {
      "process": ["process.bt"],
      "network": ["network.bt"]
    },
    "duration": 120
  }
  
  Example rule.yaml:
  - rule: container.privileged = true and container.image contains "nginx"
    desc: Detect privileged nginx containers
    condition: container.privileged = true and container.image contains "nginx"
    output: "Privileged nginx container detected: {{container.name}}"
    priority: high
    tags: ["security", "privileged", "nginx"]
    enabled: true`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			policyID := args[0]

			if policyFile == "" && ruleFile == "" {
				return fmt.Errorf("at least one of --policy or --rule flag is required")
			}

			var req UpdatePolicyRequest

			// Read and parse policy file if provided
			if policyFile != "" {
				policyData, err := os.ReadFile(policyFile)
				if err != nil {
					return fmt.Errorf("error reading policy file: %w", err)
				}

				var pf PolicyFile
				if err := json.Unmarshal(policyData, &pf); err != nil {
					return fmt.Errorf("error parsing policy file: %w", err)
				}

				req.Name = &pf.Name
				req.Metrics = pf.Metrics
				req.Duration = &pf.Duration
			}

			// Read rule file if provided (send full YAML content)
			if ruleFile != "" {
				ruleData, err := os.ReadFile(ruleFile)
				if err != nil {
					return fmt.Errorf("error reading rule file: %w", err)
				}

				// Validate that rule file has content
				if len(ruleData) == 0 {
					return fmt.Errorf("rule file is empty")
				}

				ruleStr := string(ruleData)
				req.Rule = &ruleStr // Send full YAML content
			}

			// Send request
			body, err := json.Marshal(req)
			if err != nil {
				return fmt.Errorf("error marshaling request: %w", err)
			}

			url := fmt.Sprintf("%s/api/v1/policies/%s", apiURL, policyID)
			httpReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(body))
			if err != nil {
				return fmt.Errorf("error creating request: %w", err)
			}
			httpReq.Header.Set("Content-Type", "application/json")

			client := &http.Client{}
			resp, err := client.Do(httpReq)
			if err != nil {
				return fmt.Errorf("error updating policy: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return printErrorResponse(resp)
			}

			var policy Policy
			if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
				return fmt.Errorf("error decoding response: %w", err)
			}

			fmt.Println("Policy updated successfully:")
			printPolicy(&policy)
			return nil
		},
	}

	cmd.Flags().StringVar(&policyFile, "policy", "", "Path to policy JSON file")
	cmd.Flags().StringVar(&ruleFile, "rule", "", "Path to rule YAML file")

	return cmd
}

func deletePolicyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <policy-id>",
		Short: "Delete a policy",
		Long:  `Delete a monitoring policy by its ID.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			policyID := args[0]

			url := fmt.Sprintf("%s/api/v1/policies/%s", apiURL, policyID)
			httpReq, err := http.NewRequest("DELETE", url, nil)
			if err != nil {
				return fmt.Errorf("error creating request: %w", err)
			}

			client := &http.Client{}
			resp, err := client.Do(httpReq)
			if err != nil {
				return fmt.Errorf("error deleting policy: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
				return printErrorResponse(resp)
			}

			fmt.Printf("Policy %s deleted successfully\n", policyID)
			return nil
		},
	}
}

func getPolicyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <policy-id>",
		Short: "Get a policy by ID",
		Long:  `Retrieve a monitoring policy by its ID.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			policyID := args[0]

			url := fmt.Sprintf("%s/api/v1/policies/%s", apiURL, policyID)
			resp, err := http.Get(url)
			if err != nil {
				return fmt.Errorf("error getting policy: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return printErrorResponse(resp)
			}

			var policy Policy
			if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
				return fmt.Errorf("error decoding response: %w", err)
			}

			printPolicy(&policy)
			return nil
		},
	}
}

func listPoliciesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all policies",
		Long:  `List all monitoring policies in the system.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := http.Get(apiURL + "/api/v1/policies")
			if err != nil {
				return fmt.Errorf("error listing policies: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return printErrorResponse(resp)
			}

			var result struct {
				Policies []Policy `json:"policies"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("error decoding response: %w", err)
			}

			if len(result.Policies) == 0 {
				fmt.Println("No policies found")
				return nil
			}

			fmt.Printf("Found %d policy(ies):\n", len(result.Policies))
			for i := range result.Policies {
				fmt.Printf("\n--- Policy %d ---\n", i+1)
				printPolicy(&result.Policies[i])
			}
			return nil
		},
	}
}

func printPolicy(policy *Policy) {
	fmt.Printf("  ID:        %s\n", policy.ID)
	fmt.Printf("  Name:      %s\n", policy.Name)
	fmt.Printf("  Duration:  %ds\n", policy.Duration)
	fmt.Printf("  Metrics:   %d\n", len(policy.Metrics))
	for category, scripts := range policy.Metrics {
		fmt.Printf("    - %s: %v\n", category, scripts)
	}
	fmt.Printf("  Rule:\n")

	// Parse the YAML to display rules with 'rule' field first
	var rules []map[string]interface{}
	if err := yaml.Unmarshal([]byte(policy.Rule), &rules); err == nil {
		for _, rule := range rules {
			// Print rule field first
			if ruleName, ok := rule["rule"].(string); ok {
				fmt.Printf("- rule: %s\n", ruleName)
			}
			// Print other fields in a consistent order
			if desc, ok := rule["desc"].(string); ok {
				fmt.Printf("  desc: %s\n", desc)
			}
			if condition, ok := rule["condition"].(string); ok {
				fmt.Printf("  condition: %s\n", condition)
			}
			if output, ok := rule["output"].(string); ok {
				fmt.Printf("  output: %s\n", output)
			}
			if priority, ok := rule["priority"].(string); ok {
				fmt.Printf("  priority: %s\n", priority)
			}
			if enabled, ok := rule["enabled"].(bool); ok {
				fmt.Printf("  enabled: %v\n", enabled)
			}
			if tags, ok := rule["tags"].([]interface{}); ok {
				fmt.Printf("  tags:\n")
				for _, tag := range tags {
					fmt.Printf("  - %s\n", tag)
				}
			}
		}
	} else {
		// If parsing fails, just print the raw YAML
		fmt.Printf("%s\n", policy.Rule)
	}

	fmt.Printf("  Created:   %s\n", policy.CreatedAt.Format(time.RFC3339))
	fmt.Printf("  Updated:   %s\n", policy.UpdatedAt.Format(time.RFC3339))
}

func printErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
}
