import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, userEvent, cleanup } from '../test/utils';
import { PolicyEditor } from './PolicyEditor';

describe('PolicyEditor', () => {
  const defaultProps = {
    value: '',
    onChange: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it('renders textarea for policy input', () => {
    render(<PolicyEditor {...defaultProps} />);

    expect(screen.getByRole('textbox')).toBeInTheDocument();
  });

  it('displays templates button', () => {
    render(<PolicyEditor {...defaultProps} />);

    expect(screen.getByRole('button', { name: /templates/i })).toBeInTheDocument();
  });

  it('displays format button', () => {
    render(<PolicyEditor {...defaultProps} />);

    expect(screen.getByRole('button', { name: /format/i })).toBeInTheDocument();
  });

  it('displays available S3 actions section', () => {
    render(<PolicyEditor {...defaultProps} />);

    expect(screen.getByText(/available s3 actions/i)).toBeInTheDocument();
  });

  it('lists common S3 actions', () => {
    render(<PolicyEditor {...defaultProps} />);

    expect(screen.getByText('s3:GetObject')).toBeInTheDocument();
    expect(screen.getByText('s3:PutObject')).toBeInTheDocument();
    expect(screen.getByText('s3:DeleteObject')).toBeInTheDocument();
    expect(screen.getByText('s3:ListBucket')).toBeInTheDocument();
    expect(screen.getByText('s3:CreateBucket')).toBeInTheDocument();
    expect(screen.getByText('s3:DeleteBucket')).toBeInTheDocument();
    expect(screen.getByText('s3:*')).toBeInTheDocument();
  });

  it('shows placeholder policy in textarea', () => {
    render(<PolicyEditor {...defaultProps} />);

    const textarea = screen.getByRole('textbox');
    expect(textarea).toHaveAttribute('placeholder');
  });

  it('calls onChange when text is entered', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={onChange} />);

    const textarea = screen.getByRole('textbox');
    // Escape curly braces with double braces for userEvent
    await user.type(textarea, 'test');

    expect(onChange).toHaveBeenCalled();
  });

  it('displays provided value', () => {
    const policy = JSON.stringify({ Version: '2012-10-17', Statement: [] }, null, 2);

    render(<PolicyEditor value={policy} onChange={vi.fn()} />);

    expect(screen.getByDisplayValue(/2012-10-17/)).toBeInTheDocument();
  });

  it('displays error when provided', () => {
    render(<PolicyEditor {...defaultProps} error="Policy is required" />);

    // The error should be displayed via the textarea error prop
    expect(screen.getByText('Policy is required')).toBeInTheDocument();
  });
});

describe('PolicyEditor templates', () => {
  it('opens template menu on click', async () => {
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('Insert Policy Template')).toBeInTheDocument();
    });
  });

  it('shows Read Only template option', async () => {
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('Read Only')).toBeInTheDocument();
    });
  });

  it('shows Full Access template option', async () => {
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('Full Access')).toBeInTheDocument();
    });
  });

  it('shows Bucket Admin template option', async () => {
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('Bucket Admin')).toBeInTheDocument();
    });
  });

  it('shows Write Only template option', async () => {
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('Write Only')).toBeInTheDocument();
    });
  });

  it('shows List Only template option', async () => {
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={vi.fn()} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('List Only')).toBeInTheDocument();
    });
  });

  it('applies template when clicked', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));

    await waitFor(() => {
      expect(screen.getByText('Read Only')).toBeInTheDocument();
    });

    await user.click(screen.getByText('Read Only'));

    expect(onChange).toHaveBeenCalledWith(expect.stringContaining('2012-10-17'));
    expect(onChange).toHaveBeenCalledWith(expect.stringContaining('s3:GetObject'));
  });

  it('applies Full Access template correctly', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    render(<PolicyEditor value="" onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: /templates/i }));
    await waitFor(() => {
      expect(screen.getByText('Full Access')).toBeInTheDocument();
    });

    await user.click(screen.getByText('Full Access'));

    expect(onChange).toHaveBeenCalledWith(expect.stringContaining('s3:*'));
  });
});

describe('PolicyEditor validation', () => {
  it('shows validation error for empty Version field', () => {
    const invalidPolicy = JSON.stringify({ Statement: [] }, null, 2);

    render(<PolicyEditor value={invalidPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/policy must include a "version" field/i)).toBeInTheDocument();
  });

  it('shows validation error for missing Statement array', () => {
    const invalidPolicy = JSON.stringify({ Version: '2012-10-17' }, null, 2);

    render(<PolicyEditor value={invalidPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/policy must include a "statement" array/i)).toBeInTheDocument();
  });

  it('shows validation error for invalid Effect', () => {
    const invalidPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Invalid', Action: 's3:*', Resource: '*' }],
    }, null, 2);

    render(<PolicyEditor value={invalidPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/effect must be "allow" or "deny"/i)).toBeInTheDocument();
  });

  it('shows validation error for missing Action', () => {
    const invalidPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Resource: '*' }],
    }, null, 2);

    render(<PolicyEditor value={invalidPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/missing "action" field/i)).toBeInTheDocument();
  });

  it('shows validation error for missing Resource', () => {
    const invalidPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:*' }],
    }, null, 2);

    render(<PolicyEditor value={invalidPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/missing "resource" field/i)).toBeInTheDocument();
  });

  it('shows JSON syntax error for invalid JSON', () => {
    render(<PolicyEditor value="{invalid json" onChange={vi.fn()} />);

    expect(screen.getByText(/json syntax error/i)).toBeInTheDocument();
  });

  it('shows Valid JSON indicator for valid policy', () => {
    const validPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{
        Effect: 'Allow',
        Action: 's3:*',
        Resource: '*',
      }],
    }, null, 2);

    render(<PolicyEditor value={validPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/valid json/i)).toBeInTheDocument();
  });

  it('does not show valid indicator for empty value', () => {
    render(<PolicyEditor value="" onChange={vi.fn()} />);

    expect(screen.queryByText(/valid json/i)).not.toBeInTheDocument();
  });
});

describe('PolicyEditor format button', () => {
  it('formats valid JSON when clicked', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    const unformattedJson = '{"Version":"2012-10-17","Statement":[]}';

    render(<PolicyEditor value={unformattedJson} onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: /format/i }));

    expect(onChange).toHaveBeenCalledWith(expect.stringContaining('\n'));
  });

  it('does nothing for invalid JSON', async () => {
    const onChange = vi.fn();
    const user = userEvent.setup();

    render(<PolicyEditor value="{invalid" onChange={onChange} />);

    await user.click(screen.getByRole('button', { name: /format/i }));

    // Should not call onChange since JSON is invalid
    expect(onChange).not.toHaveBeenCalled();
  });

  it('is disabled when value is empty', () => {
    render(<PolicyEditor value="" onChange={vi.fn()} />);

    expect(screen.getByRole('button', { name: /format/i })).toBeDisabled();
  });
});

describe('PolicyEditor read-only mode', () => {
  it('hides templates button in read-only mode', () => {
    const validPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:*', Resource: '*' }],
    }, null, 2);

    render(<PolicyEditor value={validPolicy} onChange={vi.fn()} readOnly />);

    expect(screen.queryByRole('button', { name: /templates/i })).not.toBeInTheDocument();
  });

  it('hides format button in read-only mode', () => {
    const validPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:*', Resource: '*' }],
    }, null, 2);

    render(<PolicyEditor value={validPolicy} onChange={vi.fn()} readOnly />);

    expect(screen.queryByRole('button', { name: /format/i })).not.toBeInTheDocument();
  });

  it('hides available actions section in read-only mode', () => {
    const validPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:*', Resource: '*' }],
    }, null, 2);

    render(<PolicyEditor value={validPolicy} onChange={vi.fn()} readOnly />);

    expect(screen.queryByText(/available s3 actions/i)).not.toBeInTheDocument();
  });

  it('makes textarea read-only', () => {
    const validPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [{ Effect: 'Allow', Action: 's3:*', Resource: '*' }],
    }, null, 2);

    render(<PolicyEditor value={validPolicy} onChange={vi.fn()} readOnly />);

    expect(screen.getByRole('textbox')).toHaveAttribute('readonly');
  });
});

describe('PolicyEditor statement validation', () => {
  it('validates multiple statements', () => {
    const policyWithMultipleStatements = JSON.stringify({
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Action: 's3:GetObject', Resource: '*' },
        { Effect: 'Invalid', Action: 's3:PutObject', Resource: '*' },
      ],
    }, null, 2);

    render(<PolicyEditor value={policyWithMultipleStatements} onChange={vi.fn()} />);

    expect(screen.getByText(/statement 2.*effect must be/i)).toBeInTheDocument();
  });

  it('validates first statement with missing Action', () => {
    const invalidPolicy = JSON.stringify({
      Version: '2012-10-17',
      Statement: [
        { Effect: 'Allow', Resource: '*' },
      ],
    }, null, 2);

    render(<PolicyEditor value={invalidPolicy} onChange={vi.fn()} />);

    expect(screen.getByText(/statement 1.*missing "action"/i)).toBeInTheDocument();
  });
});
