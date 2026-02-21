package allowlist

import "testing"

func TestIsCommandAllowedExactMatch(t *testing.T) {
	allowed := []string{"npm install", "npm run build"}

	if !IsCommandAllowed("npm install", allowed) {
		t.Fatalf("expected exact command to be allowed")
	}
	if IsCommandAllowed("npm ci", allowed) {
		t.Fatalf("did not expect unmatched command to be allowed")
	}
}

func TestIsCommandAllowedWildcardMatch(t *testing.T) {
	allowed := []string{"npm run *", "java -jar target/*.jar", "python -m *", "uvicorn *:app --host 0.0.0.0 --port ${PORT:-8000}"}

	if !IsCommandAllowed("npm run start:prod", allowed) {
		t.Fatalf("expected wildcard npm script to be allowed")
	}
	if !IsCommandAllowed("java -jar target/app.jar", allowed) {
		t.Fatalf("expected wildcard jar path to be allowed")
	}
	if !IsCommandAllowed("python -m myapp", allowed) {
		t.Fatalf("expected python module command to be allowed")
	}
	if !IsCommandAllowed("uvicorn main:app --host 0.0.0.0 --port ${PORT:-8000}", allowed) {
		t.Fatalf("expected uvicorn command to be allowed")
	}
}

func TestIsCommandAllowedWildcardRejectsUnsafeChars(t *testing.T) {
	allowed := []string{"npm run *"}

	if IsCommandAllowed("npm run build;rm", allowed) {
		t.Fatalf("did not expect unsafe wildcard token to match")
	}
	if IsCommandAllowed("npm run build -- --prod", allowed) {
		t.Fatalf("did not expect whitespace in wildcard token to match")
	}
}
