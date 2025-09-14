# Feature Deep Dive: AI-Powered Severity Scoring

This feature elevates the analysis report from a simple list of linter warnings to an intelligently prioritized action plan.

### Presentation Flow

```mermaid
flowchart TD
    A[Static Analyzer finds "Hardcoded Password"] --> B{Initial Severity: WARNING};
    B --> C[Aggregator Sends to AI Scorer];
    C --> D{Context: Issue in `config.go`};
    D --> E[LLM reasons: "A password in a config file is a critical vulnerability."];
    E --> F[New Severity: CRITICAL];
    F --> G[Issue updated with new severity and AI justification];
```

### What It Is
Static analysis tools are great at finding potential issues, but they often lack context. They might flag a hardcoded string as a "magic number" with a `WARNING` severity, regardless of whether it's a test value or a production database password.

AI-Powered Severity Scoring uses a Large Language Model (LLM) to re-evaluate the severity of issues found by static tools, taking into account the code's content and filename to make a more intelligent judgment.

### How It Works
1.  **Initial Analysis:** Static tools like `golangci-lint` run and generate a list of issues with their default severity levels.
2.  **AI Enrichment:** The `ResultAggregator` in `cmd/aggregator.go` iterates through these issues.
3.  **Prompting with Context:** For each issue, it constructs a prompt for the Gemini API. This prompt includes the issue description, the code snippet, and the filename.
4.  **AI Judgment:** The LLM is asked to rate the severity (`CRITICAL`, `ERROR`, `WARNING`, `INFO`) and provide a brief justification, considering the context.
5.  **Updating the Result:** The issue's severity is updated with the AI's more nuanced score, and the AI's reasoning is appended to the issue description.

### Use Cases & Examples

This feature helps developers focus on what truly matters.

#### Use Case 1: Prioritizing Critical Security Risks
A linter might flag any long, random-looking string as a potential hardcoded secret. The AI can differentiate.

- **Linter Output:** `Severity: WARNING`, `Issue: "G101: Potential hardcoded credentials"`
- **File Context:** The string is in a file named `production_database.go`.
- **AI Re-Scoring:** The AI recognizes the filename context and upgrades the severity.
- **Final Report:** `Severity: CRITICAL`, `Description: ... \n\n**AI Justification**: A hardcoded credential in a file named for a production environment is a critical security risk that could lead to a data breach.`

#### Use Case 2: De-prioritizing Minor Issues
Conversely, the AI can downgrade issues that are less important.

- **Linter Output:** `Severity: ERROR`, `Issue: "Function has high cyclomatic complexity"`
- **File Context:** The function is in a file named `test_helpers.go`.
- **AI Re-Scoring:** The AI understands that high complexity in a test helper function is less risky than in production logic.
- **Final Report:** `Severity: WARNING`, `Description: ... \n\n**AI Justification**: While the complexity is high, this function is part of a test suite, which reduces its immediate impact on production stability.`