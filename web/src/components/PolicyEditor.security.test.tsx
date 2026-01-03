import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '../test/utils';
import { PolicyEditor } from './PolicyEditor';

/**
 * Security Tests for PolicyEditor Component
 *
 * These tests verify protection against XSS (Cross-Site Scripting) attacks
 * and other security vulnerabilities in the PolicyEditor component.
 *
 * Test Coverage:
 * - XSS prevention in user input
 * - HTML escaping in rendered content
 * - JavaScript URL prevention
 * - Error message sanitization
 */

describe('PolicyEditor XSS Prevention', () => {
  afterEach(() => {
    cleanup();
  });

  describe('Script Injection Prevention', () => {
    it('does not execute script tags in policy value', () => {
      const xssPayload = '<script>alert("xss")</script>';
      const onChange = vi.fn();

      render(<PolicyEditor value={xssPayload} onChange={onChange} />);

      // The textarea should contain the value as text, not as executable HTML
      const textarea = screen.getByRole('textbox');
      expect(textarea).toHaveValue(xssPayload);

      // Verify no script elements were created in the DOM
      const scripts = document.getElementsByTagName('script');
      // There may be existing scripts from the testing library, but none with alert
      Array.from(scripts).forEach((script) => {
        expect(script.textContent).not.toContain('alert("xss")');
      });
    });

    it('does not execute inline event handlers in policy value', () => {
      const xssPayloads = [
        '<img src=x onerror=alert("xss")>',
        '<svg onload=alert("xss")>',
        '<body onload=alert("xss")>',
        '<div onclick=alert("xss")>click</div>',
        '<input onfocus=alert("xss") autofocus>',
      ];

      xssPayloads.forEach((payload) => {
        cleanup();
        const onChange = vi.fn();

        render(<PolicyEditor value={payload} onChange={onChange} />);

        const textarea = screen.getByRole('textbox');
        expect(textarea).toHaveValue(payload);

        // The payload should be treated as text, not as HTML
        // There should be no img, svg, or body tags with event handlers
        const imgElements = document.querySelectorAll('img[onerror]');
        expect(imgElements.length).toBe(0);
      });
    });

    it('escapes HTML entities in error messages', () => {
      // Create a value that will produce a validation error containing HTML
      const maliciousJson = '{"Version": "<script>alert(1)</script>", "Statement": []}';

      render(<PolicyEditor value={maliciousJson} onChange={vi.fn()} />);

      // Check that the script tag is not rendered as HTML
      const scriptElements = document.querySelectorAll('script');
      Array.from(scriptElements).forEach((script) => {
        expect(script.textContent).not.toContain('alert(1)');
      });
    });
  });

  describe('JavaScript URL Prevention', () => {
    it('does not execute javascript: URLs in policy content', () => {
      const jsPayloads = [
        'javascript:alert("xss")',
        'JAVASCRIPT:alert("xss")',
        'javascript:void(0)',
        'data:text/html,<script>alert("xss")</script>',
      ];

      jsPayloads.forEach((payload) => {
        cleanup();
        const onChange = vi.fn();

        render(<PolicyEditor value={payload} onChange={onChange} />);

        // Verify the value is displayed as text, not executed
        const textarea = screen.getByRole('textbox');
        expect(textarea).toHaveValue(payload);

        // No links with javascript: should be in the DOM
        const links = document.querySelectorAll('a[href^="javascript:"]');
        expect(links.length).toBe(0);
      });
    });
  });

  describe('Policy Value Sanitization', () => {
    it('handles policy with HTML in field values safely', () => {
      const policyWithHtml = JSON.stringify(
        {
          Version: '2012-10-17',
          Statement: [
            {
              Effect: 'Allow',
              Action: ['<script>alert("xss")</script>'],
              Resource: ['<img src=x onerror=alert(1)>'],
            },
          ],
        },
        null,
        2
      );

      render(<PolicyEditor value={policyWithHtml} onChange={vi.fn()} />);

      // The textarea should contain the JSON with HTML as escaped text
      const textarea = screen.getByRole('textbox');
      expect(textarea.textContent).toContain('<script>');

      // But no actual script elements should be created from the content
      const scripts = document.getElementsByTagName('script');
      Array.from(scripts).forEach((script) => {
        expect(script.textContent).not.toContain('alert("xss")');
      });
    });

    it('does not interpret JSON with prototype pollution attempts', () => {
      const maliciousPayloads = [
        '{"__proto__": {"isAdmin": true}}',
        '{"constructor": {"prototype": {"isAdmin": true}}}',
        '{"__proto__": {"polluted": true}}',
      ];

      maliciousPayloads.forEach((payload) => {
        cleanup();
        const onChange = vi.fn();

        render(<PolicyEditor value={payload} onChange={onChange} />);

        // Verify Object.prototype is not polluted
        expect(({} as Record<string, unknown>)['isAdmin']).toBeUndefined();
        expect(({} as Record<string, unknown>)['polluted']).toBeUndefined();
      });
    });
  });

  describe('Template Security', () => {
    it('templates do not contain executable code', () => {
      render(<PolicyEditor value="" onChange={vi.fn()} />);

      // All templates should be valid JSON without script content
      const templatesButton = screen.getByRole('button', { name: /templates/i });
      expect(templatesButton).toBeInTheDocument();

      // The component should render without executing any code from templates
      expect(document.body.innerHTML).not.toContain('<script>');
    });
  });

  describe('Error Message Security', () => {
    it('sanitizes error prop containing HTML', () => {
      const maliciousError = '<script>alert("xss")</script>Error message';

      render(<PolicyEditor value="" onChange={vi.fn()} error={maliciousError} />);

      // The error should be displayed as text
      expect(screen.getByText(maliciousError)).toBeInTheDocument();

      // Verify no script was executed
      const scripts = document.querySelectorAll('script');
      Array.from(scripts).forEach((script) => {
        expect(script.textContent).not.toContain('alert("xss")');
      });
    });

    it('safely displays JSON syntax error messages', () => {
      // Malformed JSON that produces an error with special characters
      const malformedJson = '{"key": "value with <script>"}';

      render(<PolicyEditor value={malformedJson} onChange={vi.fn()} />);

      // This specific JSON is valid, so test with actually malformed JSON
      const actuallyMalformedJson = '{"key": "value with <script>"';

      cleanup();
      render(<PolicyEditor value={actuallyMalformedJson} onChange={vi.fn()} />);

      // Should show a JSON syntax error message
      expect(screen.getByText(/json syntax error/i)).toBeInTheDocument();
    });
  });

  describe('Content Security', () => {
    it('treats all policy content as data, not code', () => {
      const dangerousContent = `
        {
          "Version": "2012-10-17",
          "Statement": [{
            "Effect": "Allow",
            "Action": "eval('malicious code')",
            "Resource": "Function('return this')()"
          }]
        }
      `;

      render(<PolicyEditor value={dangerousContent} onChange={vi.fn()} />);

      const textarea = screen.getByRole('textbox');
      expect(textarea).toHaveValue(dangerousContent);

      // The content should not be evaluated
      // This is inherently safe because textarea values are not executed
    });

    it('handles unicode escape sequences safely', () => {
      // Unicode escape that could be interpreted as script
      const unicodePayload = JSON.stringify({
        Version: '2012-10-17',
        Statement: [
          {
            Effect: 'Allow',
            Action: '\u003cscript\u003ealert(1)\u003c/script\u003e',
            Resource: '*',
          },
        ],
      });

      render(<PolicyEditor value={unicodePayload} onChange={vi.fn()} />);

      // Should be displayed as text, not executed
      const textarea = screen.getByRole('textbox');
      expect(textarea).toBeInTheDocument();

      // No script injection from unicode
      const scripts = document.querySelectorAll('script');
      Array.from(scripts).forEach((script) => {
        expect(script.textContent).not.toContain('alert(1)');
      });
    });

    it('handles extremely long input without crashing', () => {
      // Long input could cause DoS or buffer overflow in vulnerable implementations
      const longValue = 'a'.repeat(1000000); // 1MB of data

      expect(() => {
        render(<PolicyEditor value={longValue} onChange={vi.fn()} />);
      }).not.toThrow();

      const textarea = screen.getByRole('textbox');
      expect(textarea).toHaveValue(longValue);
    });
  });
});

describe('PolicyEditor DOM Security', () => {
  afterEach(() => {
    cleanup();
  });

  it('does not allow DOM clobbering attacks', () => {
    // DOM clobbering uses HTML elements with id/name attributes to override globals
    const clobberingPayload = '<form id="document"><input id="cookie" value="stolen"></form>';

    render(<PolicyEditor value={clobberingPayload} onChange={vi.fn()} />);

    // Verify document is not clobbered
    expect(typeof document.cookie).toBe('string');
    expect(document.getElementById).toBeDefined();
  });

  it('safely handles null bytes in content', () => {
    const nullBytePayload = 'test\x00<script>alert(1)</script>';

    expect(() => {
      render(<PolicyEditor value={nullBytePayload} onChange={vi.fn()} />);
    }).not.toThrow();
  });

  it('handles CRLF injection attempts', () => {
    const crlfPayload = 'test\r\n<script>alert(1)</script>';

    render(<PolicyEditor value={crlfPayload} onChange={vi.fn()} />);

    const textarea = screen.getByRole('textbox');
    expect(textarea).toBeInTheDocument();
  });
});
