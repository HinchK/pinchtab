#!/usr/bin/env node
/**
 * AI Code Review Script
 * Sends PR diff to Claude for review and posts results as a comment
 */

const https = require('https');
const fs = require('fs');
const { execSync } = require('child_process');

const ANTHROPIC_API_KEY = process.env.ANTHROPIC_API_KEY;
const GH_TOKEN = process.env.GH_TOKEN;
const PR_NUMBER = process.env.PR_NUMBER;

if (!ANTHROPIC_API_KEY || !GH_TOKEN || !PR_NUMBER) {
  console.error('Missing required environment variables');
  process.exit(1);
}

// Read PR diff
const diff = fs.readFileSync('pr.diff', 'utf8');

// Prepare Claude API request
const payload = JSON.stringify({
  model: 'claude-sonnet-4-5',
  max_tokens: 4096,
  messages: [{
    role: 'user',
    content: `You are a code reviewer for the pinchtab project. Review this PR diff and provide concise, actionable feedback.

Focus on:
1. Bugs or logic errors
2. Security issues (especially command injection, path traversal)
3. Performance concerns
4. Code clarity and maintainability
5. Violations of project conventions (no redundant comments, proper error handling)

Be specific about line numbers. If the code looks good, say so briefly. Keep response under 2000 characters.

PR Diff:

${diff}`
  }]
});

// Call Claude API
const options = {
  hostname: 'api.anthropic.com',
  path: '/v1/messages',
  method: 'POST',
  headers: {
    'x-api-key': ANTHROPIC_API_KEY,
    'anthropic-version': '2023-06-01',
    'content-type': 'application/json',
    'Content-Length': Buffer.byteLength(payload)
  }
};

console.log('Calling Claude API for code review...');

const req = https.request(options, (res) => {
  let data = '';

  res.on('data', (chunk) => {
    data += chunk;
  });

  res.on('end', () => {
    try {
      const response = JSON.parse(data);
      
      if (response.error) {
        console.error('Claude API error:', response.error);
        process.exit(1);
      }

      const reviewText = response.content[0].text;
      console.log('Review received from Claude');

      // Post comment to PR
      const comment = `## 🤖 AI Code Review (Claude)

${reviewText}

---
*Automated review using Claude Sonnet 4.5*`;

      execSync(`gh pr comment ${PR_NUMBER} --body ${JSON.stringify(comment)}`, {
        env: { ...process.env, GH_TOKEN }
      });

      console.log('Review posted to PR');
    } catch (err) {
      console.error('Error processing response:', err);
      console.error('Response:', data);
      process.exit(1);
    }
  });
});

req.on('error', (err) => {
  console.error('Request error:', err);
  process.exit(1);
});

req.write(payload);
req.end();
