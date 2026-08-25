package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/CycloneDX/cyclonedx-go"
	"github.com/ncruces/zenity"
	"github.com/xuri/excelize/v2"
	"golang.org/x/mod/modfile"
)

// createHTTPClient creates a standardized HTTP client with timeout settings
func createHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			DisableCompression:    false,
			DisableKeepAlives:     false,
			ResponseHeaderTimeout: 5 * time.Second,
		},
	}
}

// cleanVersionString removes comparison operators and cleans up version strings
func cleanVersionString(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, ">=")
	version = strings.TrimPrefix(version, "==")
	version = strings.TrimPrefix(version, ">")
	version = strings.TrimPrefix(version, "<=")
	version = strings.TrimPrefix(version, "<")
	version = strings.TrimPrefix(version, "~=")
	version = strings.TrimPrefix(version, "^")
	version = strings.TrimPrefix(version, "~")
	version = strings.Split(version, ",")[0] // Take first part if multiple constraints
	version = strings.Split(version, " ")[0] // Take first part if space separated
	// Unpinned versions resolve to the latest release on the registry
	if version == "*" || version == "latest" || version == "" {
		return ""
	}
	return version
}

// standardizeLicense converts various license formats to standard SPDX identifiers
func standardizeLicense(licenseName string) string {
	// Clean up common license abbreviations and variations
	switch licenseName {
	case "Apache Software License":
		return "Apache-2.0"
	case "BSD License":
		return "BSD-3-Clause"
	case "MIT License":
		return "MIT"
	case "Mozilla Public License 2.0 (MPL 2.0)":
		return "MPL-2.0"
	case "GNU General Public License v3 (GPLv3)":
		return "GPL-3.0"
	case "GNU General Public License v2 (GPLv2)":
		return "GPL-2.0"
	case "GNU Lesser General Public License v3 (LGPLv3)":
		return "LGPL-3.0"
	case "GNU Lesser General Public License v2 (LGPLv2)":
		return "LGPL-2.0"
	default:
		// Try to match common patterns
		if strings.Contains(licenseName, "Apache") {
			return "Apache-2.0"
		} else if strings.Contains(licenseName, "MIT") {
			return "MIT"
		} else if strings.Contains(licenseName, "BSD") {
			return "BSD-3-Clause"
		} else if strings.Contains(licenseName, "GPL") && strings.Contains(licenseName, "3") {
			return "GPL-3.0"
		} else if strings.Contains(licenseName, "GPL") && strings.Contains(licenseName, "2") {
			return "GPL-2.0"
		}
		return licenseName
	}
}

// extractGitHubLink extracts GitHub repository link from various sources
func extractGitHubLink(projectURLs map[string]string, homepage string) (string, string) {
	var repository, githubURL string

	// Check project URLs for GitHub link
	for key, url := range projectURLs {
		if strings.Contains(strings.ToLower(url), "github") {
			githubURL = url
		}
		// Also check for common repository keys
		if strings.Contains(strings.ToLower(key), "source") ||
			strings.Contains(strings.ToLower(key), "repository") {
			repository = url
		}
	}

	// Use homepage if no repository found
	if repository == "" && homepage != "" {
		repository = homepage
	}

	// If GitHub URL not found but repository has GitHub, use it
	if githubURL == "" && strings.Contains(strings.ToLower(repository), "github") {
		githubURL = repository
	}

	return repository, githubURL
}

// setCopyrightFromLicense sets copyright information based on license
func setCopyrightFromLicense(license string) string {
	if license != "" {
		return license + " Copyright"
	}
	return ""
}

// buildLicenseURL builds an SPDX license URL from a license identifier.
// Non-SPDX values (containing spaces or other characters) yield an empty URL
// because they cannot be mapped to a stable reference.
func buildLicenseURL(license string) string {
	if license == "" {
		return ""
	}
	for _, r := range license {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
		default:
			return ""
		}
	}
	return "https://spdx.org/licenses/" + license + ".html"
}

// findLatestVersion finds the latest version from releases map
func findLatestVersion(releases map[string][]struct {
	PythonVersion string `json:"python_version"`
	UploadTime    string `json:"upload_time"`
}) string {
	latestVersion := ""
	latestTime := ""
	for ver, releaseList := range releases {
		if len(releaseList) > 0 {
			uploadTime := releaseList[0].UploadTime
			if uploadTime > latestTime {
				latestVersion = ver
				latestTime = uploadTime
			}
		}
	}
	return latestVersion
}

type PackageInfo struct {
	Name            string
	Version         string
	License         string
	LicenseURL      string
	Author          string
	Description     string
	Copyright       string
	PackageURL      string
	GitHubURL       string
	RepositoryType  string
	Repository      string
	ModuleNameNoVer string
}

// Package represents a dependency
type Package struct {
	Path      string
	Version   string
	GoMod     bool
	PyProject bool
}

// Parse go.mod file
func parseGoMod(filename string) ([]Package, string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", err
	}

	// Use ParseLax to allow unknown block types
	file, err := modfile.ParseLax(filepath.Base(filename), data, nil)
	if err != nil {
		return nil, "", err
	}

	var packages []Package
	for _, req := range file.Require {
		packages = append(packages, Package{
			Path:    req.Mod.Path,
			Version: req.Mod.Version,
			GoMod:   true,
		})
	}

	// Get module name from the parsed file. A go.mod without a module
	// directive parses fine with ParseLax but has a nil Module; fall back to
	// the file name instead of panicking.
	moduleName := filepath.Base(filename)
	if file.Module != nil {
		moduleName = file.Module.Mod.Path
	}
	moduleName += "-api"
	return packages, moduleName, nil
}

// Parse package.json file
func parsePackageJSON(filename string) ([]Package, string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", err
	}

	var packageJSON struct {
		Name            string            `json:"name"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal(data, &packageJSON); err != nil {
		return nil, "", err
	}

	var packages []Package

	for name, version := range packageJSON.Dependencies {
		packages = append(packages, Package{
			Path:    name,
			Version: version,
			GoMod:   false,
		})
	}

	for name, version := range packageJSON.DevDependencies {
		packages = append(packages, Package{
			Path:    name,
			Version: version,
			GoMod:   false,
		})
	}

	return packages, packageJSON.Name + "-ui", nil
}

// Parse pyproject.toml file
func parsePyProjectToml(filename string) ([]Package, string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", err
	}

	var pyProject struct {
		Project struct {
			Name         string   `toml:"name"`
			Dependencies []string `toml:"dependencies"`
		} `toml:"project"`
		Tool struct {
			Poetry struct {
				Name            string            `toml:"name"`
				Dependencies    map[string]string `toml:"dependencies"`
				DevDependencies map[string]string `toml:"dev-dependencies"`
			} `toml:"poetry"`
		} `toml:"tool"`
		BuildSystem struct {
			Requires []string `toml:"requires"`
		} `toml:"build-system"`
	}

	if err := toml.Unmarshal(data, &pyProject); err != nil {
		return nil, "", err
	}

	var packages []Package

	// Handle Poetry dependencies
	if pyProject.Tool.Poetry.Dependencies != nil {
		for name, version := range pyProject.Tool.Poetry.Dependencies {
			// Skip poetry itself and special entries
			if name == "python" || strings.Contains(name, "poetry") {
				continue
			}
			packages = append(packages, Package{
				Path:      name,
				Version:   version,
				GoMod:     false,
				PyProject: true,
			})
		}
	}

	// Handle Poetry dev-dependencies
	if pyProject.Tool.Poetry.DevDependencies != nil {
		for name, version := range pyProject.Tool.Poetry.DevDependencies {
			// Skip poetry itself and special entries
			if name == "python" || strings.Contains(name, "poetry") {
				continue
			}
			packages = append(packages, Package{
				Path:      name,
				Version:   version,
				GoMod:     false,
				PyProject: true,
			})
		}
	}

	// Handle PEP 621 dependencies (project.dependencies)
	if len(pyProject.Project.Dependencies) > 0 {
		for _, dep := range pyProject.Project.Dependencies {
			// Parse dependency string like "requests>=2.0.0" or "numpy==1.19.0"
			parts := strings.Fields(dep)
			if len(parts) > 0 {
				name := parts[0]
				version := ""
				if len(parts) > 1 {
					version = strings.Join(parts[1:], " ")
				}
				packages = append(packages, Package{
					Path:      name,
					Version:   version,
					GoMod:     false,
					PyProject: true,
				})
			}
		}
	}

	// Determine project name
	projectName := "python-project"
	if pyProject.Tool.Poetry.Name != "" {
		projectName = pyProject.Tool.Poetry.Name
	} else if pyProject.Project.Name != "" {
		projectName = pyProject.Project.Name
	}

	return packages, projectName + "-py", nil
}

// Get metadata from PyPI
func getPyPI_Metadata(pkg *Package) PackageInfo {
	info := PackageInfo{
		Name:            pkg.Path,
		Version:         pkg.Version,
		ModuleNameNoVer: pkg.Path,
		RepositoryType:  "pypi",
	}

	// Clean version string - remove comparison operators
	version := cleanVersionString(pkg.Version)

	// Create HTTP client with timeout
	client := createHTTPClient()

	// Get info from PyPI API with context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First try to get package info
	reqURL := "https://pypi.org/pypi/" + pkg.Path + "/json"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return info
	}

	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return info
	}
	defer resp.Body.Close()

	var pypiPkg struct {
		Info struct {
			Author       string            `json:"author"`
			AuthorEmail  string            `json:"author_email"`
			Classifiers  []string          `json:"classifiers"`
			Description  string            `json:"description"`
			Summary      string            `json:"summary"`
			Home_page    string            `json:"home_page"`
			License      string            `json:"license"`
			Version      string            `json:"version"`
			Project_urls map[string]string `json:"project_urls"`
		} `json:"info"`
		Releases map[string][]struct {
			PythonVersion string `json:"python_version"`
			UploadTime    string `json:"upload_time"`
		} `json:"releases"`
		URLs []struct {
			Packagetype string `json:"packagetype"`
			URL         string `json:"url"`
		} `json:"urls"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pypiPkg); err == nil {
		// First, look for license in classifiers (more reliable)
		for _, classifier := range pypiPkg.Info.Classifiers {
			if strings.HasPrefix(classifier, "License :: ") {
				parts := strings.Split(classifier, " :: ")
				if len(parts) >= 3 {
					// Extract the license name (last part)
					licenseName := parts[len(parts)-1]
					info.License = standardizeLicense(licenseName)
					info.LicenseURL = buildLicenseURL(info.License)
					break
				}
			}
		}

		// If no license found in classifiers, try license field
		if info.License == "" && pypiPkg.Info.License != "" {
			info.License = standardizeLicense(pypiPkg.Info.License)
			info.LicenseURL = buildLicenseURL(info.License)
		}

		// Get author
		if pypiPkg.Info.Author != "" {
			info.Author = pypiPkg.Info.Author
		} else if pypiPkg.Info.AuthorEmail != "" {
			info.Author = pypiPkg.Info.AuthorEmail
		}

		// Get description
		if pypiPkg.Info.Summary != "" {
			info.Description = pypiPkg.Info.Summary
		} else if pypiPkg.Info.Description != "" {
			info.Description = pypiPkg.Info.Description
		}

		// Get repository URL
		if pypiPkg.Info.Home_page != "" {
			info.Repository = pypiPkg.Info.Home_page
			info.GitHubURL = pypiPkg.Info.Home_page
		}

		// Extract GitHub and repository links from project URLs
		repository, githubURL := extractGitHubLink(pypiPkg.Info.Project_urls, pypiPkg.Info.Home_page)
		if repository != "" {
			info.Repository = repository
		}
		if githubURL != "" {
			info.GitHubURL = githubURL
		}

		// Set copyright if we have license
		info.Copyright = setCopyrightFromLicense(info.License)

		// Prefer the exact version published on PyPI over the constraint floor
		// from the manifest (e.g. "requests>=2.31.0" should not be reported as
		// 2.31.0). An explicit exact pin (bare "2.31.0" or "==2.31.0") is kept
		// as-is; anything else resolves to the latest published release.
		trimmed := strings.TrimSpace(pkg.Version)
		exactPin := version != "" && (trimmed == version || strings.HasPrefix(trimmed, "=="))
		if exactPin {
			info.Version = version
		} else if pypiPkg.Info.Version != "" {
			info.Version = pypiPkg.Info.Version
		} else if version == "" && len(pypiPkg.Releases) > 0 {
			latestVersion := findLatestVersion(pypiPkg.Releases)
			if latestVersion != "" {
				info.Version = latestVersion
			}
		}
	}

	return info
}

// getGoModMetadata fetches module metadata from the official pkg.go.dev v1 API
// (https://pkg.go.dev/v1/module/{path}) instead of scraping HTML, which was
// fragile against frontend changes.
func getGoModMetadata(pkg *Package) PackageInfo {
	info := PackageInfo{
		Name:           pkg.Path,
		Version:        pkg.Version,
		PackageURL:     pkg.Path + "/@v/" + pkg.Version + ".info",
		RepositoryType: "go",
	}

	version := cleanVersionString(pkg.Version)
	client := createHTTPClient()

	// Module endpoint: licenses (SPDX types), license text, repo URL.
	// Each request gets its own timeout budget: the module endpoint can take
	// several seconds on a cold cache, which would otherwise starve the
	// package request when they share one context.
	moduleURL := "https://pkg.go.dev/v1/module/" + pkg.Path + "?licenses=true"
	if version != "" {
		moduleURL += "&version=" + url.QueryEscape(version)
	}

	var mod struct {
		Version  string `json:"version"`
		RepoURL  string `json:"repoUrl"`
		Licenses []struct {
			Types    []string `json:"types"`
			Contents string   `json:"contents"`
		} `json:"licenses"`
	}
	// pkg.go.dev occasionally returns an empty licenses list from a cold
	// cache; one retry is cheap and avoids a missing license in the report.
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, "GET", moduleURL, nil)
		if err == nil {
			if resp, err := client.Do(req); err == nil && resp.StatusCode == 200 {
				err = json.NewDecoder(resp.Body).Decode(&mod)
				resp.Body.Close()
				if err == nil && len(mod.Licenses) > 0 {
					cancel()
					break
				}
			}
		}
		cancel()
		mod = struct {
			Version  string `json:"version"`
			RepoURL  string `json:"repoUrl"`
			Licenses []struct {
				Types    []string `json:"types"`
				Contents string   `json:"contents"`
			} `json:"licenses"`
		}{}
	}

	if len(mod.Licenses) > 0 && len(mod.Licenses[0].Types) > 0 {
		info.License = mod.Licenses[0].Types[0]
		info.LicenseURL = buildLicenseURL(info.License)
		info.Copyright = setCopyrightFromLicense(info.License)
		// Prefer the actual copyright line from the license text
		for line := range strings.SplitSeq(mod.Licenses[0].Contents, "\n") {
			l := strings.TrimSpace(line)
			if strings.Contains(strings.ToLower(l), "copyright") || strings.Contains(l, "©") {
				info.Copyright = l
				break
			}
		}
	}

	if mod.RepoURL != "" {
		info.Repository = mod.RepoURL
		info.GitHubURL = mod.RepoURL
	}

	if mod.Version != "" {
		info.Version = mod.Version
	}

	// Package endpoint: synopsis as description (own context, see above)
	pkgURL := "https://pkg.go.dev/v1/package/" + pkg.Path
	if version != "" {
		pkgURL += "?version=" + url.QueryEscape(version)
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req2, err := http.NewRequestWithContext(ctx, "GET", pkgURL, nil)
		if err == nil {
			if resp2, err := client.Do(req2); err == nil && resp2.StatusCode == 200 {
				var pkgDoc struct {
					Synopsis string `json:"synopsis"`
				}
				if err := json.NewDecoder(resp2.Body).Decode(&pkgDoc); err == nil {
					info.Description = pkgDoc.Synopsis
				}
				resp2.Body.Close()
			}
		}
	}

	// The API does not store developer names; infer from the module path
	if strings.Contains(pkg.Path, "github.com/") {
		parts := strings.Split(pkg.Path, "/")
		if len(parts) >= 2 {
			info.Author = parts[1]
		}
	}
	if info.Author == "" && strings.Contains(pkg.Path, "/") {
		parts := strings.Split(pkg.Path, "/")
		if len(parts) >= 2 {
			info.Author = parts[0]
		}
	}

	return info
}

// Get metadata from npm registry
func getNPMMetadata(pkg *Package) PackageInfo {
	info := PackageInfo{
		Name:            pkg.Path,
		Version:         pkg.Version,
		ModuleNameNoVer: pkg.Path,
		RepositoryType:  "npm",
	}

	// Without a pinned version the registry resolves "latest" to the newest
	// release and returns a full version document
	version := cleanVersionString(pkg.Version)
	if version == "" {
		version = "latest"
	}

	// Create HTTP client with timeout
	client := createHTTPClient()

	// Get info from npm registry with context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	regURL := "https://registry.npmjs.org/" + pkg.Path + "/" + version
	req, err := http.NewRequestWithContext(ctx, "GET", regURL, nil)
	if err != nil {
		return info
	}

	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		var npmPkg struct {
			License  string `json:"license"`
			Version  string `json:"version"`
			Licenses []struct {
				Type string `json:"type"`
			} `json:"licenses"`
			Author      any                 `json:"author"`
			Maintainers []map[string]string `json:"maintainers"`
			Description string              `json:"description"`
			Repository  struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"repository"`
			Homepage string `json:"homepage"`
			Readme   string `json:"readme"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&npmPkg); err == nil {
			// Get license
			if npmPkg.License != "" {
				info.License = npmPkg.License
				info.LicenseURL = buildLicenseURL(npmPkg.License)
			} else if len(npmPkg.Licenses) > 0 {
				info.License = npmPkg.Licenses[0].Type
				info.LicenseURL = buildLicenseURL(npmPkg.Licenses[0].Type)
			}

			// Get author - npm metadata shapes vary; never panic on unexpected types
			switch a := npmPkg.Author.(type) {
			case map[string]any:
				if name, ok := a["name"].(string); ok && name != "" {
					info.Author = name
				} else if email, ok := a["email"].(string); ok && email != "" {
					info.Author = email
				}
			case string:
				if a != "" {
					info.Author = a
				}
			}

			// If no author from main field, try maintainers
			if info.Author == "" && len(npmPkg.Maintainers) > 0 {
				if name, ok := npmPkg.Maintainers[0]["name"]; ok {
					info.Author = name
				} else if email, ok := npmPkg.Maintainers[0]["email"]; ok {
					info.Author = email
				}
			}

			// Resolve the actual version when the manifest spec was not an
			// exact release: "*", "latest", "", "^x.y.z", "~x.y.z" and dist
			// tags all resolve to the concrete version the registry served.
			// For an exact pin the registry echoes the same version back.
			if npmPkg.Version != "" && npmPkg.Version != info.Version {
				info.Version = npmPkg.Version
			}

			info.Description = npmPkg.Description

			// Get repository/GitHub URL
			if npmPkg.Repository.URL != "" {
				info.Repository = npmPkg.Repository.URL
				info.GitHubURL = npmPkg.Repository.URL
			} else if npmPkg.Homepage != "" {
				info.Repository = npmPkg.Homepage
			}

			// Set copyright from license
			info.Copyright = setCopyrightFromLicense(info.License)

			// If no license found, try to extract from README
			if info.License == "" && npmPkg.Readme != "" {
				// Try to find copyright mentions in README
				for line := range strings.SplitSeq(npmPkg.Readme, "\n") {
					if strings.Contains(strings.ToLower(line), "copyright") ||
						strings.Contains(line, "©") {
						info.Copyright = strings.TrimSpace(line)
						break
					}
				}
			}
		}
	}

	return info
}

// buildPURL constructs a package URL (https://github.com/package-url/purl-spec)
// for the three supported ecosystems. Returns "" for unknown types.
func buildPURL(repoType, name, version string) string {
	switch repoType {
	case "go":
		if version == "" {
			return "pkg:golang/" + name
		}
		return "pkg:golang/" + name + "@" + version
	case "npm":
		// Scoped packages must be encoded: @babel/core -> %40babel%2Fcore
		n := strings.ReplaceAll(name, "@", "%40")
		n = strings.ReplaceAll(n, "/", "%2F")
		if version == "" {
			return "pkg:npm/" + n
		}
		return "pkg:npm/" + n + "@" + version
	case "pypi":
		// PyPI normalizes names to lowercase with hyphens
		n := strings.ToLower(name)
		n = strings.ReplaceAll(n, "_", "-")
		if version == "" {
			return "pkg:pypi/" + n
		}
		return "pkg:pypi/" + n + "@" + version
	}
	return ""
}

// moduleResult groups the fetched metadata of one parsed manifest file
type moduleResult struct {
	moduleName string
	infos      []PackageInfo
}

// buildSBOM assembles a CycloneDX 1.6 BOM from all parsed modules.
// Scope is limited to top-level (direct) dependencies per the CRA minimum
// requirement; transitive dependencies would need lockfile parsing.
// Duplicate packages across modules are merged by PURL.
func buildSBOM(modules []moduleResult) *cyclonedx.BOM {
	bom := cyclonedx.NewBOM()
	bom.SpecVersion = cyclonedx.SpecVersion1_6
	bom.JSONSchema = "http://cyclonedx.org/schema/bom-1.6.schema.json"

	var mainName string
	for _, m := range modules {
		mainName += m.moduleName + "+"
	}
	mainName = strings.TrimSuffix(mainName, "+")

	bom.Metadata = &cyclonedx.Metadata{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Component: &cyclonedx.Component{
			Type: cyclonedx.ComponentTypeApplication,
			Name: mainName,
		},
	}

	components := []cyclonedx.Component{}
	seen := map[string]bool{}
	for _, m := range modules {
		for _, info := range m.infos {
			purl := buildPURL(info.RepositoryType, info.Name, info.Version)
			if purl == "" || seen[purl] {
				continue
			}
			seen[purl] = true

			comp := cyclonedx.Component{
				Type:        cyclonedx.ComponentTypeLibrary,
				Name:        info.Name,
				Version:     info.Version,
				BOMRef:      purl,
				PackageURL:  purl,
				Description: info.Description,
				Copyright:   info.Copyright,
			}
			if info.License != "" {
				comp.Licenses = &cyclonedx.Licenses{{
					License: &cyclonedx.License{
						ID:  info.License,
						URL: info.LicenseURL,
					},
				}}
			}
			if info.Author != "" {
				comp.Authors = &[]cyclonedx.OrganizationalContact{{Name: info.Author}}
			}
			components = append(components, comp)
		}
	}
	if len(components) > 0 {
		bom.Components = &components
	}
	return bom
}

func main() {
	wd, err := os.Getwd()
	if err != nil {
		zenity.Error("Failed to get current working directory: "+err.Error(), zenity.Title("Error"), zenity.ErrorIcon)
		return
	}

	inFiles, err := zenity.SelectFileMultiple(
		zenity.Filename(wd),
		zenity.FileFilters{
			{
				Name:     "All Supported Format",
				Patterns: []string{"go.mod", "package.json", "pyproject.toml"},
				CaseFold: false,
			},
			{
				Name:     "Go Module",
				Patterns: []string{"go.mod"},
				CaseFold: false,
			},
			{
				Name:     "Package JSON",
				Patterns: []string{"package.json"},
				CaseFold: false,
			},
			{
				Name:     "Python Project",
				Patterns: []string{"pyproject.toml"},
				CaseFold: false,
			},
		},
	)
	if err != nil {
		// User cancelled - exit process instead of showing error dialog
		os.Exit(1)
	}

	// Parse every selected manifest file
	type parsedFile struct {
		inName      string
		moduleName  string
		isGoMod     bool
		isPyProject bool
		packages    []Package
	}
	var parsed []parsedFile
	for _, inName := range inFiles {
		isGoMod := strings.HasSuffix(inName, "go.mod")
		isPyProject := strings.HasSuffix(inName, "pyproject.toml")

		var moduleName string
		var packages []Package
		if isGoMod {
			packages, moduleName, err = parseGoMod(inName)
		} else if isPyProject {
			packages, moduleName, err = parsePyProjectToml(inName)
		} else {
			packages, moduleName, err = parsePackageJSON(inName)
		}
		if err != nil {
			zenity.Error("Failed to parse "+inName+": "+err.Error(), zenity.Title("Error"), zenity.ErrorIcon)
			return
		}
		parsed = append(parsed, parsedFile{
			inName:      inName,
			moduleName:  moduleName,
			isGoMod:     isGoMod,
			isPyProject: isPyProject,
			packages:    packages,
		})
	}

	dlg, err := zenity.Progress(
		zenity.Title("Running..."))
	if err != nil {
		zenity.Error("Create progress dialog failed: "+err.Error(), zenity.Title("Error"), zenity.ErrorIcon)
		os.Exit(1)
	}
	defer dlg.Close()

	total := 0
	for _, pf := range parsed {
		total += len(pf.packages)
	}
	processed := 0

	// Fetch metadata for every package; collect rows for the single summary
	// Excel report and per-module lists for the merged SBOM
	type excelRow struct {
		sourceFile string
		info       PackageInfo
	}
	var rows []excelRow
	var modules []moduleResult
	for _, pf := range parsed {
		infos := make([]PackageInfo, 0, len(pf.packages))
		for _, pkg := range pf.packages {
			if total > 0 {
				dlg.Value(int(float64(processed) / float64(total) * 100))
			}
			dlg.Text("Processing " + pkg.Path + "...")
			processed++

			var info PackageInfo
			if pf.isGoMod {
				info = getGoModMetadata(&pkg)
			} else if pf.isPyProject {
				info = getPyPI_Metadata(&pkg)
			} else {
				info = getNPMMetadata(&pkg)
			}
			infos = append(infos, info)
			rows = append(rows, excelRow{sourceFile: filepath.Base(pf.inName), info: info})
		}
		modules = append(modules, moduleResult{moduleName: pf.moduleName, infos: infos})
	}

	// Single summary Excel report covering every selected manifest file
	f := excelize.NewFile()
	sheetName := f.GetSheetName(0)
	header := []string{"Source File", "Name", "Version", "License", "License URL", "Author", "Description", "Copyright", "Repository", "GitHub URL", "Repository Type"}
	for i, col := range header {
		cell := fmt.Sprintf("%s1", string(rune('A'+i)))
		f.SetCellValue(sheetName, cell, col)
	}
	for i, r := range rows {
		row := []interface{}{
			r.sourceFile,
			r.info.Name,
			r.info.Version,
			r.info.License,
			r.info.LicenseURL,
			r.info.Author,
			r.info.Description,
			r.info.Copyright,
			r.info.Repository,
			r.info.GitHubURL,
			r.info.RepositoryType,
		}
		for j, val := range row {
			cell := fmt.Sprintf("%s%d", string(rune('A'+j)), i+2)
			f.SetCellValue(sheetName, cell, val)
		}
	}
	if err := f.SaveAs("license_report.xlsx"); err != nil {
		zenity.Error("Failed to save Excel file: "+err.Error(), zenity.Title("Error"), zenity.ErrorIcon)
		return
	}

	// Assemble the merged CycloneDX SBOM covering all selected manifests
	bom := buildSBOM(modules)
	sbomOut, err := os.Create("sbom.json")
	if err != nil {
		zenity.Error("Failed to create SBOM file: "+err.Error(), zenity.Title("Error"), zenity.ErrorIcon)
		return
	}
	defer sbomOut.Close()
	encoder := cyclonedx.NewBOMEncoder(sbomOut, cyclonedx.BOMFileFormatJSON)
	encoder.SetPretty(true)
	if err := encoder.Encode(bom); err != nil {
		zenity.Error("Failed to write SBOM: "+err.Error(), zenity.Title("Error"), zenity.ErrorIcon)
		return
	}

	dlg.Complete()
	zenity.Info("License report: license_report.xlsx. SBOM: sbom.json", zenity.Title("Success"), zenity.InfoIcon)
}
