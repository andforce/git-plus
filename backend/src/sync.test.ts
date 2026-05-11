import { describe, expect, it } from 'vitest';
import { githubGitAuthHeader } from './sync';
import { redactTokenText } from './util';

describe('git sync auth helpers', () => {
  it('builds the GitHub HTTPS Basic auth header expected by git', () => {
    const header = githubGitAuthHeader('ghp_example');

    expect(header).toBe(
      `Authorization: Basic ${Buffer.from(
        'x-access-token:ghp_example',
        'utf8',
      ).toString('base64')}`,
    );
  });

  it('redacts tokens and authorization headers from error text', () => {
    const token = 'ghp_exampleSecret123';
    const fineGrainedToken = 'github_pat_exampleSecret456';
    const output = redactTokenText(
      [
        `Command failed with ${token}`,
        `Authorization: Bearer ${token}`,
        `Authorization: Basic ${Buffer.from(
          `x-access-token:${token}`,
          'utf8',
        ).toString('base64')}`,
        `remote said ${fineGrainedToken}`,
      ].join('\n'),
      [token],
    );

    expect(output).not.toContain(token);
    expect(output).not.toContain(fineGrainedToken);
    expect(output).not.toContain('Authorization: Bearer');
    expect(output).not.toContain('Authorization: Basic');
    expect(output).toContain('[redacted-token]');
  });
});
