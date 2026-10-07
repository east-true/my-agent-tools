package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/google/go-github/v92/github"
)

type DependabotOptions struct {
	State     string `json:"state"`
	Severity  string `json:"severity,omitempty"`
	Ecosystem string `json:"ecosystem,omitempty"`
	Package   string `json:"package,omitempty"`
}

func (options *DependabotOptions) Normalize() error {
	if options.State == "" {
		options.State = "open"
	}
	for _, filter := range []struct {
		name    string
		value   *string
		allowed []string
	}{
		{"state", &options.State, []string{"open", "fixed", "dismissed", "auto_dismissed", "all"}},
		{"severity", &options.Severity, []string{"low", "medium", "high", "critical"}},
	} {
		if *filter.value == "" {
			continue
		}
		parts := strings.Split(*filter.value, ",")
		for i, part := range parts {
			parts[i] = strings.TrimSpace(part)
			valid := false
			for _, allowed := range filter.allowed {
				if parts[i] == allowed {
					valid = true
				}
			}
			if !valid {
				return fmt.Errorf("--%s must use %s (comma-separated)", filter.name, strings.Join(filter.allowed, ", "))
			}
			if parts[i] == "all" && len(parts) > 1 {
				return errors.New("--state all must be used alone")
			}
		}
		*filter.value = strings.Join(parts, ",")
	}
	return nil
}

// DependabotAlert contains the affected dependency and the patch for this alert,
// rather than the advisory's patches for other affected release lines.
type DependabotAlert struct {
	Number           int      `json:"number"`
	State            string   `json:"state"`
	Package          string   `json:"package"`
	Ecosystem        string   `json:"ecosystem"`
	ManifestPath     string   `json:"manifest_path"`
	Scope            string   `json:"scope"`
	Relationship     string   `json:"relationship,omitempty"`
	Severity         string   `json:"severity"`
	Summary          string   `json:"summary"`
	VulnerableRange  string   `json:"vulnerable_version_range"`
	PatchedVersion   *string  `json:"first_patched_version"`
	GHSAID           string   `json:"ghsa_id"`
	CVEID            string   `json:"cve_id,omitempty"`
	URL              string   `json:"url"`
	Description      string   `json:"description,omitempty"`
	References       []string `json:"references,omitempty"`
	DismissedReason  string   `json:"dismissed_reason,omitempty"`
	DismissedComment string   `json:"dismissed_comment,omitempty"`
}

type dependabotResource struct {
	Number     int    `json:"number"`
	State      string `json:"state"`
	URL        string `json:"html_url"`
	Dependency struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		ManifestPath string `json:"manifest_path"`
		Scope        string `json:"scope"`
		Relationship string `json:"relationship"`
	} `json:"dependency"`
	Advisory struct {
		GHSAID      string `json:"ghsa_id"`
		CVEID       string `json:"cve_id"`
		Summary     string `json:"summary"`
		Description string `json:"description"`
		Severity    string `json:"severity"`
		References  []struct {
			URL string `json:"url"`
		} `json:"references"`
	} `json:"security_advisory"`
	Vulnerability struct {
		Severity        string `json:"severity"`
		VulnerableRange string `json:"vulnerable_version_range"`
		PatchedVersion  *struct {
			Identifier string `json:"identifier"`
		} `json:"first_patched_version"`
	} `json:"security_vulnerability"`
	DismissedReason  string `json:"dismissed_reason"`
	DismissedComment string `json:"dismissed_comment"`
}

func (resource dependabotResource) alert(detail bool) DependabotAlert {
	alert := DependabotAlert{
		Number: resource.Number, State: resource.State, URL: resource.URL,
		Package: resource.Dependency.Package.Name, Ecosystem: resource.Dependency.Package.Ecosystem,
		ManifestPath: resource.Dependency.ManifestPath, Scope: resource.Dependency.Scope,
		Relationship: resource.Dependency.Relationship, Severity: resource.Vulnerability.Severity,
		Summary: resource.Advisory.Summary, GHSAID: resource.Advisory.GHSAID, CVEID: resource.Advisory.CVEID,
		VulnerableRange: resource.Vulnerability.VulnerableRange,
	}
	if alert.Severity == "" {
		alert.Severity = resource.Advisory.Severity
	}
	if patch := resource.Vulnerability.PatchedVersion; patch != nil && patch.Identifier != "" {
		alert.PatchedVersion = &patch.Identifier
	}
	if detail {
		alert.Description = resource.Advisory.Description
		alert.DismissedReason = resource.DismissedReason
		alert.DismissedComment = resource.DismissedComment
		for _, ref := range resource.Advisory.References {
			alert.References = append(alert.References, ref.URL)
		}
	}
	return alert
}

func (client Client) ListDependabot(ctx context.Context, repo string, options DependabotOptions) ([]DependabotAlert, error) {
	if err := options.Normalize(); err != nil {
		return nil, err
	}
	api, ok := client.API.(CursorAPI)
	if !ok {
		return nil, errors.New("GitHub API does not support cursor pagination")
	}
	query := url.Values{"per_page": {"100"}, "sort": {"created"}, "direction": {"desc"}}
	if options.State != "all" {
		query.Set("state", options.State)
	}
	for key, value := range map[string]string{"severity": options.Severity, "ecosystem": options.Ecosystem, "package": options.Package} {
		if value != "" {
			query.Set(key, value)
		}
	}
	alerts := []DependabotAlert{}
	seen := map[string]bool{}
	for {
		var page []dependabotResource
		next, err := api.GetCursorPage(ctx, "repos/"+repo+"/dependabot/alerts?"+query.Encode(), &page)
		if err != nil {
			return nil, dependabotError(err)
		}
		for _, resource := range page {
			alerts = append(alerts, resource.alert(false))
		}
		if next == "" {
			return alerts, nil
		}
		if seen[next] {
			return nil, errors.New("Dependabot pagination returned a repeated cursor")
		}
		seen[next] = true
		query.Set("after", next)
	}
}

func (client Client) ViewDependabot(ctx context.Context, repo string, number int) (DependabotAlert, error) {
	if number <= 0 {
		return DependabotAlert{}, errors.New("--number must be a positive alert number")
	}
	var resource dependabotResource
	err := client.api(ctx, http.MethodGet, fmt.Sprintf("repos/%s/dependabot/alerts/%d", repo, number), nil, &resource)
	if err != nil {
		return DependabotAlert{}, dependabotError(err)
	}
	return resource.alert(true), nil
}

func dependabotError(err error) error {
	var response *sdk.ErrorResponse
	if errors.As(err, &response) && response.Response != nil {
		switch response.Response.StatusCode {
		case http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("read Dependabot alerts: check repository/alert access, Dependabot alerts enablement, and token permissions (fine-grained: Dependabot alerts read; classic: security_events or public_repo for public repositories): %w", err)
		}
	}
	return fmt.Errorf("read Dependabot alerts: %w", err)
}
