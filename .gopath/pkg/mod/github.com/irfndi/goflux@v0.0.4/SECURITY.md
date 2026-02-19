# Security Policy

## Reporting a Vulnerability

If you believe you have found a security vulnerability in GoFlux, please report it privately.

- Email: irfandimarsya@gmail.com
- Include: a description of the issue, impact, steps to reproduce, and any proof-of-concept code
- Response: We will acknowledge receipt of your report within 48 hours

Please do not open a public GitHub issue for security reports.

**Coordinated Disclosure**: We appreciate responsible disclosure and will work with you to understand and fix the issue.

## Supported Versions

Security fixes are provided for:

- The latest released version
- The `main` branch

## Security Policy

### Vulnerability Handling Process

1. **Report Received**: We acknowledge security reports within 48 hours
2. **Assessment**: We assess and triage the vulnerability within 7 business days
3. **Fix Development**: We develop and test the fix
4. **Release**: We release a security update with the fix
5. **Public Disclosure**: We disclose the vulnerability after the fix is deployed

### Severity Levels

- **Critical**: Security vulnerability that allows unauthorized access or data theft
- **High**: Security vulnerability with significant impact
- **Medium**: Security vulnerability with limited impact
- **Low**: Security vulnerability with minimal impact or workaround available

### Security Best Practices

When contributing to GoFlux:

- Do not commit credentials, API keys, or secrets
- Use environment variables for sensitive configuration
- Follow secure coding practices
- Report suspicious security issues privately
- Perform security reviews for changes affecting security-sensitive areas

### Dependency Management

We use Dependabot for automated dependency updates. Security vulnerabilities in dependencies are tracked and addressed through GitHub's Dependabot alerts.

### Encryption and Transport

- The project uses HTTPS for all communications
- Dependencies are fetched from HTTPS sources (go modules)
- No hardcoded credentials or secrets in the codebase

## Security Audits

GoFlux is designed to follow security best practices:

- Input validation for all external data
- Safe error handling without exposing internal details
- Proper nil checks to prevent panics
- No use of unsafe or deprecated functions
- Regular dependency updates via Dependabot

## Contact Information

For security-related questions or concerns:

- Email: irfandimarsya@gmail.com
- GitHub Security: https://github.com/irfndi/goflux/security

For non-security bugs, please use the normal issue tracker.
