# Security Policy

## Reporting Security Vulnerabilities

We take the security of Daz seriously. If you discover a security vulnerability, please follow these steps:

### 1. Do NOT Create a Public Issue

Security vulnerabilities should **never** be reported via public GitHub issues, as this could put users at risk.

### 2. Report Via Private Channel

Please report security vulnerabilities by emailing the maintainer directly:

**Email**: [Create a security contact email or use GitHub Security Advisories]

Include the following information:
- Description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Any suggested fixes (if applicable)

### 3. Response Time

We will acknowledge receipt of your vulnerability report within 48 hours and will send a more detailed response within 7 days indicating the next steps in handling your report.

## Security Best Practices for Users

### Credential Management

1. **Never commit credentials to version control**
   - Use environment variables via `.env` file
   - Keep `.env`, `config.json`, and `rooms.json` files private
   - Ensure these files are in `.gitignore`

2. **Use strong, unique passwords**
   - Database passwords should be complex
   - Bot passwords should be unique per instance
   - Never reuse passwords across services

3. **Limit database permissions**
   - Create a dedicated database user for Daz
   - Grant only necessary permissions (CREATE, SELECT, INSERT, UPDATE, DELETE)
   - Avoid using superuser accounts

### Configuration Security

1. **Protect configuration files**
   ```bash
   chmod 600 .env config.json rooms.json
   ```

2. **Use secure channels**
   - Only connect to trusted CyTube channels
   - Be cautious with admin permissions

3. **Regular updates**
   - Keep Daz updated to the latest version
   - Monitor for security advisories

### Database Security

1. **Use SSL/TLS for database connections** when possible
2. **Restrict database access** to localhost or specific IPs
3. **Regular backups** of your database
4. **Monitor for suspicious activity** in logs

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < 1.0   | :x:                |

We actively maintain the `main` branch. Users should keep their installations up to date.

## Security Features

Daz includes several security features:

- Environment variable support for sensitive configuration
- Configurable admin users list
- Plugin isolation via EventBus architecture
- Database connection pooling with limits
- Comprehensive error handling

## Acknowledgments

We appreciate responsible disclosure of security vulnerabilities. Security researchers who report valid issues will be acknowledged in our release notes (unless they prefer to remain anonymous).