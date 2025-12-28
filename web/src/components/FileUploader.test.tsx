import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '../test/utils';
import { FileUploader } from './FileUploader';

// Mock the console API
vi.mock('../api/client', () => ({
  consoleApi: {
    uploadObjectWithProgress: vi.fn(),
  },
}));

// Mock mantine notifications
vi.mock('@mantine/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@mantine/notifications')>();
  return {
    ...actual,
    notifications: {
      ...actual.notifications,
      show: vi.fn(),
    },
  };
});

// Mock @mantine/dropzone - must define everything inline since vi.mock is hoisted
vi.mock('@mantine/dropzone', () => {
  // Create mock sub-components
  const MockDropzoneAccept = ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dropzone-accept">{children}</div>
  );
  const MockDropzoneReject = ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dropzone-reject">{children}</div>
  );
  const MockDropzoneIdle = ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dropzone-idle">{children}</div>
  );

  // Create the main Dropzone mock with sub-components attached
  const MockDropzone = ({ children, disabled }: { children: React.ReactNode; disabled?: boolean }) => (
    <div data-testid="dropzone" data-disabled={disabled}>
      {children}
    </div>
  );
  MockDropzone.Accept = MockDropzoneAccept;
  MockDropzone.Reject = MockDropzoneReject;
  MockDropzone.Idle = MockDropzoneIdle;

  return {
    Dropzone: MockDropzone,
    FileWithPath: {},
    FileRejection: {},
  };
});

describe('FileUploader', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the component', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.getByTestId('dropzone')).toBeInTheDocument();
  });

  it('renders dropzone area with text', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.getByText(/drag files here or click to select/i)).toBeInTheDocument();
  });

  it('displays upload constraints message with custom maxFiles', () => {
    render(<FileUploader bucket="test-bucket" maxFiles={5} />);
    expect(screen.getByText(/upload up to 5 files/i)).toBeInTheDocument();
  });

  it('shows default max files (10)', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.getByText(/upload up to 10 files/i)).toBeInTheDocument();
  });

  it('displays file size limit for 100MB', () => {
    render(<FileUploader bucket="test-bucket" maxSize={1024 * 1024 * 100} />);
    expect(screen.getByText(/100\.0 MB/i)).toBeInTheDocument();
  });

  it('displays file size limit for 1KB', () => {
    render(<FileUploader bucket="test-bucket" maxSize={1024} />);
    expect(screen.getByText(/1\.0 KB/)).toBeInTheDocument();
  });

  it('displays file size limit for 1MB', () => {
    render(<FileUploader bucket="test-bucket" maxSize={1024 * 1024} />);
    expect(screen.getByText(/1\.0 MB/)).toBeInTheDocument();
  });

  it('displays file size limit for 1GB', () => {
    render(<FileUploader bucket="test-bucket" maxSize={1024 * 1024 * 1024} />);
    expect(screen.getByText(/1\.00 GB/)).toBeInTheDocument();
  });

  it('displays file size limit for bytes', () => {
    render(<FileUploader bucket="test-bucket" maxSize={512} />);
    expect(screen.getByText(/512 B/)).toBeInTheDocument();
  });

  it('uses default 5GB max size', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.getByText(/5\.00 GB/)).toBeInTheDocument();
  });

  it('renders with custom prefix', () => {
    render(<FileUploader bucket="test-bucket" prefix="uploads/" />);
    expect(screen.getByTestId('dropzone')).toBeInTheDocument();
  });

  it('does not show upload button initially', () => {
    render(<FileUploader bucket="test-bucket" />);
    // No files selected, so no upload button
    expect(screen.queryByRole('button', { name: /upload/i })).not.toBeInTheDocument();
  });

  it('does not show clear completed button initially', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.queryByText(/clear completed/i)).not.toBeInTheDocument();
  });

  it('does not show file list initially', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.queryByText(/file\(s\) selected/)).not.toBeInTheDocument();
  });

  it('accepts custom file types prop', () => {
    render(
      <FileUploader
        bucket="test-bucket"
        accept={['image/png', 'image/jpeg']}
      />
    );
    expect(screen.getByTestId('dropzone')).toBeInTheDocument();
  });

  it('displays help text', () => {
    render(<FileUploader bucket="test-bucket" />);
    expect(screen.getByText(/drag files here or click to select/i)).toBeInTheDocument();
  });
});
