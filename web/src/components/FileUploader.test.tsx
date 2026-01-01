import { describe, it, expect, vi, beforeEach, afterEach, Mock } from 'vitest';
import { render, screen, cleanup, waitFor, fireEvent, act } from '../test/utils';
import { FileUploader } from './FileUploader';
import { consoleApi } from '../api/client';

// =============================================================================
// Test Constants
// =============================================================================
// File size constants for consistent test values
const TEST_FILE_SIZES = {
  TINY: 100,           // 100 bytes - for rejection tests
  SMALL: 512,          // 512 bytes
  STANDARD: 1024,      // 1 KB - default test file size
  MEDIUM: 1536,        // 1.5 KB - for size display tests
  LARGE: 2048,         // 2 KB
  VERY_LARGE: 10240,   // 10 KB - for progress tracking tests
} as const;

// File size limits for validation tests
const TEST_SIZE_LIMITS = {
  TINY: 100,                      // 100 bytes - very restrictive
  ONE_KB: 1024,                   // 1 KB
  ONE_MB: 1024 * 1024,            // 1 MB
  ONE_HUNDRED_MB: 1024 * 1024 * 100, // 100 MB
  ONE_GB: 1024 * 1024 * 1024,     // 1 GB
  FIVE_GB: 1024 * 1024 * 1024 * 5, // 5 GB - default max
} as const;

// Test file configuration
const TEST_MAX_FILES = {
  DEFAULT: 10,
  SMALL: 2,
  MEDIUM: 5,
} as const;

// Mock the console API
vi.mock('../api/client', () => ({
  consoleApi: {
    uploadObjectWithProgress: vi.fn(),
  },
}));

// Use vi.hoisted to ensure mock function is available before vi.mock hoisting
const mockShowNotification = vi.hoisted(() => vi.fn());

// Mock mantine notifications
vi.mock('@mantine/notifications', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@mantine/notifications')>();
  return {
    ...actual,
    notifications: {
      ...actual.notifications,
      show: mockShowNotification,
    },
  };
});

// Global registry for dropzone callbacks - hoisted to ensure availability
const dropzoneRegistry = vi.hoisted(() => ({
  onDrop: null as ((files: File[]) => void) | null,
  onReject: null as ((rejections: Array<{ file: File; errors: Array<{ code: string; message: string }> }>) => void) | null,
  maxSize: undefined as number | undefined,
  accept: undefined as string[] | undefined,
  disabled: false,
}));

// Create a more functional Dropzone mock that allows simulating file operations
vi.mock('@mantine/dropzone', () => {
  interface DropzoneProps {
    children: React.ReactNode;
    disabled?: boolean;
    onDrop: (files: File[]) => void;
    onReject: (rejections: Array<{ file: File; errors: Array<{ code: string; message: string }> }>) => void;
    maxSize?: number;
    maxFiles?: number;
    accept?: string[];
  }

  const MockDropzoneAccept = ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dropzone-accept">{children}</div>
  );
  const MockDropzoneReject = ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dropzone-reject">{children}</div>
  );
  const MockDropzoneIdle = ({ children }: { children: React.ReactNode }) => (
    <div data-testid="dropzone-idle">{children}</div>
  );

  /*
   * ESLint disable explanation (react-hooks/immutability):
   * This mock component intentionally stores props in a global registry to enable
   * test code to simulate file drops without complex event simulation.
   * This pattern is necessary because:
   * 1. The real Dropzone uses native file input which can't be programmatically triggered
   * 2. Tests need to call onDrop/onReject callbacks directly to simulate user actions
   * 3. The registry approach is cleaner than alternative patterns like ref forwarding
   * This side effect is isolated to test code and doesn't affect production behavior.
   */
  /* eslint-disable react-hooks/immutability */
  const MockDropzone = (props: DropzoneProps) => {
    const { children, disabled, onDrop, onReject, maxSize, accept } = props;

    // Capture component props in registry for test simulation
    dropzoneRegistry.onDrop = onDrop;
    dropzoneRegistry.onReject = onReject;
    dropzoneRegistry.maxSize = maxSize;
    dropzoneRegistry.accept = accept;
    dropzoneRegistry.disabled = disabled || false;

    return (
      <div
        data-testid="dropzone"
        data-disabled={disabled}
      >
        {children}
      </div>
    );
  };
  /* eslint-enable react-hooks/immutability */

  MockDropzone.Accept = MockDropzoneAccept;
  MockDropzone.Reject = MockDropzoneReject;
  MockDropzone.Idle = MockDropzoneIdle;

  return {
    Dropzone: MockDropzone,
    FileWithPath: {},
    FileRejection: {},
  };
});

// Helper to create mock files
function createMockFile(
  name: string,
  size: number,
  type = 'text/plain'
): File {
  const content = new Array(size).fill('a').join('');
  const file = new File([content], name, { type });
  Object.defineProperty(file, 'size', { value: size });
  return file;
}

// Helper to simulate file selection using the dropzone registry
function simulateFileSelect(_dropzone: HTMLElement, files: File[]) {
  if (dropzoneRegistry.disabled) return;

  const acceptedFiles: File[] = [];
  const rejectedFiles: Array<{
    file: File;
    errors: Array<{ code: string; message: string }>;
  }> = [];

  files.forEach((file) => {
    const errors: Array<{ code: string; message: string }> = [];

    // Check file size
    if (dropzoneRegistry.maxSize && file.size > dropzoneRegistry.maxSize) {
      errors.push({
        code: 'file-too-large',
        message: `File is larger than ${dropzoneRegistry.maxSize} bytes`,
      });
    }

    // Check file type
    if (
      dropzoneRegistry.accept &&
      dropzoneRegistry.accept.length > 0 &&
      !dropzoneRegistry.accept.includes(file.type)
    ) {
      errors.push({
        code: 'file-invalid-type',
        message: `File type ${file.type} is not accepted`,
      });
    }

    if (errors.length > 0) {
      rejectedFiles.push({ file, errors });
    } else {
      acceptedFiles.push(file);
    }
  });

  if (acceptedFiles.length > 0 && dropzoneRegistry.onDrop) {
    dropzoneRegistry.onDrop(acceptedFiles);
  }
  if (rejectedFiles.length > 0 && dropzoneRegistry.onReject) {
    dropzoneRegistry.onReject(rejectedFiles);
  }
}

describe('FileUploader', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockShowNotification.mockClear();
  });

  afterEach(() => {
    cleanup();
  });

  // =========================================
  // Basic Rendering Tests
  // =========================================
  describe('Basic Rendering', () => {
    it('renders the component', () => {
      render(<FileUploader bucket="test-bucket" />);
      expect(screen.getByTestId('dropzone')).toBeInTheDocument();
    });

    it('renders dropzone area with text', () => {
      render(<FileUploader bucket="test-bucket" />);
      expect(screen.getByText(/drag files here or click to select/i)).toBeInTheDocument();
    });

    it('displays upload constraints message with custom maxFiles', () => {
      render(<FileUploader bucket="test-bucket" maxFiles={TEST_MAX_FILES.MEDIUM} />);
      expect(screen.getByText(/upload up to 5 files/i)).toBeInTheDocument();
    });

    it('shows default max files (10)', () => {
      render(<FileUploader bucket="test-bucket" />);
      expect(screen.getByText(/upload up to 10 files/i)).toBeInTheDocument();
    });

    it('displays file size limit for 100MB', () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_SIZE_LIMITS.ONE_HUNDRED_MB} />);
      expect(screen.getByText(/100\.0 MB/i)).toBeInTheDocument();
    });

    it('displays file size limit for 1KB', () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_SIZE_LIMITS.ONE_KB} />);
      expect(screen.getByText(/1\.0 KB/)).toBeInTheDocument();
    });

    it('displays file size limit for 1MB', () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_SIZE_LIMITS.ONE_MB} />);
      expect(screen.getByText(/1\.0 MB/)).toBeInTheDocument();
    });

    it('displays file size limit for 1GB', () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_SIZE_LIMITS.ONE_GB} />);
      expect(screen.getByText(/1\.00 GB/)).toBeInTheDocument();
    });

    it('displays file size limit for bytes', () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_FILE_SIZES.SMALL} />);
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

  // =========================================
  // Upload Flow Tests
  // =========================================
  describe('Upload Flow', () => {
    it('shows file list after adding files', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByText('test-file.txt')).toBeInTheDocument();
        expect(screen.getByText(/1 file\(s\) selected/)).toBeInTheDocument();
      });
    });

    it('shows upload button after adding files', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });
    });

    it('uploads file successfully and updates status', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'test-file.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      const uploadButton = screen.getByRole('button', { name: /upload 1 file/i });

      await act(async () => {
        fireEvent.click(uploadButton);
      });

      await waitFor(() => {
        expect(mockUpload).toHaveBeenCalledWith(
          'test-bucket',
          expect.any(File),
          'test-file.txt',
          expect.any(Function)
        );
      });

      await waitFor(() => {
        expect(screen.getByText('completed')).toBeInTheDocument();
      });
    });

    it('uploads file with custom prefix', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'uploads/test-file.txt' } });

      render(<FileUploader bucket="test-bucket" prefix="uploads/" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      const uploadButton = screen.getByRole('button', { name: /upload 1 file/i });

      await act(async () => {
        fireEvent.click(uploadButton);
      });

      await waitFor(() => {
        expect(mockUpload).toHaveBeenCalledWith(
          'test-bucket',
          expect.any(File),
          'uploads/test-file.txt',
          expect.any(Function)
        );
      });
    });

    it('calls onUploadComplete callback after successful upload', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'test-file.txt' } });
      const onUploadComplete = vi.fn();

      render(<FileUploader bucket="test-bucket" onUploadComplete={onUploadComplete} />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(onUploadComplete).toHaveBeenCalled();
      });
    });

    it('shows success notification after all files upload', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'test-file.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(mockShowNotification).toHaveBeenCalledWith(
          expect.objectContaining({
            title: 'Upload complete',
            color: 'green',
          })
        );
      });
    });
  });

  // =========================================
  // Progress Tracking Tests
  // =========================================
  describe('Progress Tracking', () => {
    it('shows uploading status during upload', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      let progressCallback: ((progress: number) => void) | undefined;

      mockUpload.mockImplementation(
        (
          _bucket: string,
          _file: File,
          _path: string,
          onProgress: (progress: number) => void
        ) => {
          progressCallback = onProgress;
          return new Promise((resolve) => {
            setTimeout(() => resolve({ data: { key: 'test-file.txt' } }), 100);
          });
        }
      );

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      // During upload, status should show as 0% (initial progress)
      await waitFor(() => {
        expect(screen.getByText('0%')).toBeInTheDocument();
      });

      // Simulate progress update
      await act(async () => {
        if (progressCallback) {
          progressCallback(50);
        }
      });

      await waitFor(() => {
        expect(screen.getByText('50%')).toBeInTheDocument();
      });
    });

    it('updates progress percentage as upload progresses', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      let progressCallback: ((progress: number) => void) | undefined;
      let resolveUpload: (value: { data: { key: string } }) => void;

      mockUpload.mockImplementation(
        (
          _bucket: string,
          _file: File,
          _path: string,
          onProgress: (progress: number) => void
        ) => {
          progressCallback = onProgress;
          return new Promise((resolve) => {
            resolveUpload = resolve;
          });
        }
      );

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.VERY_LARGE);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      // Simulate progress updates
      await act(async () => {
        if (progressCallback) {
          progressCallback(25);
        }
      });

      await waitFor(() => {
        expect(screen.getByText('25%')).toBeInTheDocument();
      });

      await act(async () => {
        if (progressCallback) {
          progressCallback(75);
        }
      });

      await waitFor(() => {
        expect(screen.getByText('75%')).toBeInTheDocument();
      });

      // Complete the upload
      await act(async () => {
        resolveUpload({ data: { key: 'test-file.txt' } });
      });

      await waitFor(() => {
        expect(screen.getByText('completed')).toBeInTheDocument();
      });
    });

    it('shows progress bar during upload', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      let resolveUpload: (value: { data: { key: string } }) => void;

      mockUpload.mockImplementation(() => {
        return new Promise((resolve) => {
          resolveUpload = resolve;
        });
      });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      // During upload, a progress bar should be visible (role="progressbar")
      await waitFor(() => {
        expect(screen.getByRole('progressbar')).toBeInTheDocument();
      });

      // Complete the upload
      await act(async () => {
        resolveUpload({ data: { key: 'test-file.txt' } });
      });

      // Progress bar should disappear after completion
      await waitFor(() => {
        expect(screen.queryByRole('progressbar')).not.toBeInTheDocument();
      });
    });
  });

  // =========================================
  // File Removal/Cancellation Tests
  // =========================================
  describe('File Removal and Cancellation', () => {
    it('allows removing pending files before upload', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByText('test-file.txt')).toBeInTheDocument();
      });

      // Find and click the remove button
      const removeButton = screen.getByRole('button', { name: '' });

      await act(async () => {
        fireEvent.click(removeButton);
      });

      await waitFor(() => {
        expect(screen.queryByText('test-file.txt')).not.toBeInTheDocument();
      });
    });

    it('clears completed files when clicking clear completed button', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'test-file.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText('completed')).toBeInTheDocument();
      });

      // Click clear completed button
      const clearButton = screen.getByRole('button', { name: /clear completed/i });

      await act(async () => {
        fireEvent.click(clearButton);
      });

      await waitFor(() => {
        expect(screen.queryByText('test-file.txt')).not.toBeInTheDocument();
      });
    });

    it('allows removing error files', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockRejectedValueOnce(new Error('Upload failed'));

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText('error')).toBeInTheDocument();
      });

      // Find and click the remove button for the error file
      const removeButtons = screen.getAllByRole('button', { name: '' });
      const removeButton = removeButtons[removeButtons.length - 1];

      await act(async () => {
        fireEvent.click(removeButton);
      });

      await waitFor(() => {
        expect(screen.queryByText('test-file.txt')).not.toBeInTheDocument();
      });
    });
  });

  // =========================================
  // Error Handling Tests
  // =========================================
  describe('Error Handling', () => {
    it('shows error status when upload fails', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockRejectedValueOnce(new Error('Network error'));

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText('error')).toBeInTheDocument();
      });
    });

    it('displays error message when upload fails', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockRejectedValueOnce(new Error('Server error: 500'));

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText('Server error: 500')).toBeInTheDocument();
      });
    });

    it('shows generic error message for non-Error exceptions', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockRejectedValueOnce('Some string error');

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText('Upload failed')).toBeInTheDocument();
      });
    });

    it('shows partial completion notification when some files fail', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload
        .mockResolvedValueOnce({ data: { key: 'file1.txt' } })
        .mockRejectedValueOnce(new Error('Upload failed'));

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file1 = createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD);
      const file2 = createMockFile('file2.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file1, file2]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 2 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 2 file/i }));
      });

      // Wait for both uploads to complete and notification to be shown
      await waitFor(
        () => {
          expect(mockShowNotification).toHaveBeenCalledWith(
            expect.objectContaining({
              title: 'Upload partially complete',
              color: 'yellow',
            })
          );
        },
        { timeout: 3000 }
      );
    });

    it('shows notification when file is rejected', async () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_SIZE_LIMITS.TINY} />);

      const dropzone = screen.getByTestId('dropzone');
      // Create a file larger than maxSize (STANDARD > TINY)
      const oversizedFile = createMockFile('large-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [oversizedFile]);
      });

      await waitFor(() => {
        expect(mockShowNotification).toHaveBeenCalledWith(
          expect.objectContaining({
            title: 'File rejected',
            color: 'red',
          })
        );
      });
    });

    it('handles timeout errors gracefully', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      const timeoutError = new Error('timeout of 30000ms exceeded');
      timeoutError.name = 'AxiosError';
      mockUpload.mockRejectedValueOnce(timeoutError);

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText('error')).toBeInTheDocument();
        expect(screen.getByText(/timeout/i)).toBeInTheDocument();
      });
    });
  });

  // =========================================
  // Multi-file Upload Tests
  // =========================================
  describe('Multi-file Uploads', () => {
    it('handles multiple file selection', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file1 = createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD);
      const file2 = createMockFile('file2.txt', TEST_FILE_SIZES.LARGE);
      const file3 = createMockFile('file3.txt', TEST_FILE_SIZES.SMALL);

      act(() => {
        simulateFileSelect(dropzone, [file1, file2, file3]);
      });

      await waitFor(() => {
        expect(screen.getByText('file1.txt')).toBeInTheDocument();
        expect(screen.getByText('file2.txt')).toBeInTheDocument();
        expect(screen.getByText('file3.txt')).toBeInTheDocument();
        expect(screen.getByText(/3 file\(s\) selected/)).toBeInTheDocument();
      });
    });

    it('uploads multiple files sequentially', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      const uploadOrder: string[] = [];

      mockUpload.mockImplementation(
        (_bucket: string, file: File) => {
          uploadOrder.push(file.name);
          return Promise.resolve({ data: { key: file.name } });
        }
      );

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file1 = createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD);
      const file2 = createMockFile('file2.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file1, file2]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 2 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 2 file/i }));
      });

      await waitFor(() => {
        expect(mockUpload).toHaveBeenCalledTimes(2);
      });

      // Files should be uploaded in order
      expect(uploadOrder).toEqual(['file1.txt', 'file2.txt']);
    });

    it('respects maxFiles limit', async () => {
      render(<FileUploader bucket="test-bucket" maxFiles={TEST_MAX_FILES.SMALL} />);

      const dropzone = screen.getByTestId('dropzone');
      const files = [
        createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD),
        createMockFile('file2.txt', TEST_FILE_SIZES.STANDARD),
        createMockFile('file3.txt', TEST_FILE_SIZES.STANDARD),
      ];

      act(() => {
        simulateFileSelect(dropzone, files);
      });

      await waitFor(() => {
        // Should only have 2 files
        expect(screen.getByText(/2 file\(s\) selected/)).toBeInTheDocument();
      });
    });

    it('shows correct count for pending files', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const files = [
        createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD),
        createMockFile('file2.txt', TEST_FILE_SIZES.STANDARD),
        createMockFile('file3.txt', TEST_FILE_SIZES.STANDARD),
      ];

      act(() => {
        simulateFileSelect(dropzone, files);
      });

      await waitFor(() => {
        expect(screen.getByText(/3 pending/)).toBeInTheDocument();
      });
    });

    it('shows completed count after uploads finish', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValue({ data: { key: 'test.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const files = [
        createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD),
        createMockFile('file2.txt', TEST_FILE_SIZES.STANDARD),
      ];

      act(() => {
        simulateFileSelect(dropzone, files);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 2 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 2 file/i }));
      });

      await waitFor(() => {
        expect(screen.getByText(/2 completed/)).toBeInTheDocument();
      });
    });
  });

  // =========================================
  // File Validation Tests
  // =========================================
  describe('File Validation', () => {
    it('rejects files larger than maxSize', async () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_SIZE_LIMITS.ONE_KB} />);

      const dropzone = screen.getByTestId('dropzone');
      const largeFile = createMockFile('large-file.txt', TEST_FILE_SIZES.LARGE);

      act(() => {
        simulateFileSelect(dropzone, [largeFile]);
      });

      await waitFor(() => {
        expect(mockShowNotification).toHaveBeenCalledWith(
          expect.objectContaining({
            title: 'File rejected',
            color: 'red',
          })
        );
      });

      // File should not be in the list
      expect(screen.queryByText('large-file.txt')).not.toBeInTheDocument();
    });

    it('rejects files with invalid type', async () => {
      render(
        <FileUploader
          bucket="test-bucket"
          accept={['image/png', 'image/jpeg']}
        />
      );

      const dropzone = screen.getByTestId('dropzone');
      const textFile = createMockFile('document.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [textFile]);
      });

      await waitFor(() => {
        expect(mockShowNotification).toHaveBeenCalledWith(
          expect.objectContaining({
            title: 'File rejected',
            color: 'red',
          })
        );
      });
    });

    it('accepts files within size limit', async () => {
      render(<FileUploader bucket="test-bucket" maxSize={TEST_FILE_SIZES.LARGE} />);

      const dropzone = screen.getByTestId('dropzone');
      const validFile = createMockFile('small-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [validFile]);
      });

      await waitFor(() => {
        expect(screen.getByText('small-file.txt')).toBeInTheDocument();
      });
    });

    it('accepts files with valid type', async () => {
      render(
        <FileUploader
          bucket="test-bucket"
          accept={['image/png', 'image/jpeg']}
        />
      );

      const dropzone = screen.getByTestId('dropzone');
      const pngFile = new File(['test'], 'image.png', { type: 'image/png' });
      Object.defineProperty(pngFile, 'size', { value: TEST_FILE_SIZES.STANDARD });

      act(() => {
        simulateFileSelect(dropzone, [pngFile]);
      });

      await waitFor(() => {
        expect(screen.getByText('image.png')).toBeInTheDocument();
      });
    });

    it('displays file size correctly for selected files', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.MEDIUM); // 1.5 KB

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByText('1.5 KB')).toBeInTheDocument();
      });
    });
  });

  // =========================================
  // Disabled State Tests
  // =========================================
  describe('Disabled State', () => {
    it('disables dropzone during upload', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      let resolveUpload: (value: { data: { key: string } }) => void;

      mockUpload.mockImplementation(() => {
        return new Promise((resolve) => {
          resolveUpload = resolve;
        });
      });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      // Dropzone should be disabled during upload
      await waitFor(() => {
        expect(screen.getByTestId('dropzone')).toHaveAttribute('data-disabled', 'true');
      });

      // Complete the upload
      await act(async () => {
        resolveUpload({ data: { key: 'test-file.txt' } });
      });

      // Dropzone should be enabled after upload
      await waitFor(() => {
        expect(screen.getByTestId('dropzone')).not.toHaveAttribute('data-disabled', 'true');
      });
    });

    it('hides upload button when all files are uploading', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      let resolveUpload: (value: { data: { key: string } }) => void;

      mockUpload.mockImplementation(() => {
        return new Promise((resolve) => {
          resolveUpload = resolve;
        });
      });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('test-file.txt', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /upload 1 file/i })).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      // Upload button should disappear when no more pending files
      // (the file status changes from 'pending' to 'uploading')
      await waitFor(() => {
        expect(screen.queryByRole('button', { name: /upload.*file/i })).not.toBeInTheDocument();
      });

      // Complete the upload
      await act(async () => {
        resolveUpload({ data: { key: 'test-file.txt' } });
      });
    });
  });

  // =========================================
  // Edge Cases
  // =========================================
  describe('Edge Cases', () => {
    it('handles empty file correctly', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'empty.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const emptyFile = createMockFile('empty.txt', 0);

      act(() => {
        simulateFileSelect(dropzone, [emptyFile]);
      });

      await waitFor(() => {
        expect(screen.getByText('empty.txt')).toBeInTheDocument();
        expect(screen.getByText('0 B')).toBeInTheDocument();
      });
    });

    it('handles special characters in filename', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValueOnce({ data: { key: 'file with spaces & symbols!.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const file = createMockFile('file with spaces & symbols!.txt', 1024);

      act(() => {
        simulateFileSelect(dropzone, [file]);
      });

      await waitFor(() => {
        expect(screen.getByText('file with spaces & symbols!.txt')).toBeInTheDocument();
      });

      await act(async () => {
        fireEvent.click(screen.getByRole('button', { name: /upload 1 file/i }));
      });

      await waitFor(() => {
        expect(mockUpload).toHaveBeenCalledWith(
          'test-bucket',
          expect.any(File),
          'file with spaces & symbols!.txt',
          expect.any(Function)
        );
      });
    });

    it('does not start upload when no pending files', async () => {
      const mockUpload = consoleApi.uploadObjectWithProgress as Mock;
      mockUpload.mockResolvedValue({ data: { key: 'test.txt' } });

      render(<FileUploader bucket="test-bucket" />);

      // No files selected, should not have upload button
      expect(screen.queryByRole('button', { name: /upload/i })).not.toBeInTheDocument();
    });

    it('handles rapid file additions correctly', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');

      // Add files rapidly
      await act(async () => {
        simulateFileSelect(dropzone, [createMockFile('file1.txt', TEST_FILE_SIZES.STANDARD)]);
        simulateFileSelect(dropzone, [createMockFile('file2.txt', TEST_FILE_SIZES.STANDARD)]);
        simulateFileSelect(dropzone, [createMockFile('file3.txt', TEST_FILE_SIZES.STANDARD)]);
      });

      await waitFor(() => {
        expect(screen.getByText(/3 file\(s\) selected/)).toBeInTheDocument();
      });
    });
  });

  // =========================================
  // Drag and Drop Behavior Tests
  // =========================================
  describe('Drag and Drop Behavior', () => {
    it('simulates drag and drop file addition', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const droppedFile = createMockFile('dropped-file.txt', TEST_FILE_SIZES.STANDARD);

      // Simulate drag-over state (visual feedback)
      fireEvent.dragOver(dropzone);

      // Simulate file drop using our registry-based simulation
      act(() => {
        simulateFileSelect(dropzone, [droppedFile]);
      });

      await waitFor(() => {
        expect(screen.getByText('dropped-file.txt')).toBeInTheDocument();
      });
    });

    it('handles multiple files dropped at once', async () => {
      render(<FileUploader bucket="test-bucket" />);

      const dropzone = screen.getByTestId('dropzone');
      const droppedFiles = [
        createMockFile('photo1.jpg', TEST_FILE_SIZES.STANDARD),
        createMockFile('photo2.jpg', TEST_FILE_SIZES.STANDARD),
        createMockFile('photo3.jpg', TEST_FILE_SIZES.STANDARD),
      ];

      act(() => {
        simulateFileSelect(dropzone, droppedFiles);
      });

      await waitFor(() => {
        expect(screen.getByText('photo1.jpg')).toBeInTheDocument();
        expect(screen.getByText('photo2.jpg')).toBeInTheDocument();
        expect(screen.getByText('photo3.jpg')).toBeInTheDocument();
        expect(screen.getByText(/3 file\(s\) selected/)).toBeInTheDocument();
      });
    });

    it('rejects invalid files during drop', async () => {
      render(
        <FileUploader
          bucket="test-bucket"
          accept={['image/png', 'image/jpeg']}
        />
      );

      const dropzone = screen.getByTestId('dropzone');
      const invalidFile = createMockFile('document.pdf', TEST_FILE_SIZES.STANDARD);

      act(() => {
        simulateFileSelect(dropzone, [invalidFile]);
      });

      await waitFor(() => {
        expect(mockShowNotification).toHaveBeenCalledWith(
          expect.objectContaining({
            title: 'File rejected',
            color: 'red',
          })
        );
      });

      // File should not appear in the list
      expect(screen.queryByText('document.pdf')).not.toBeInTheDocument();
    });
  });
});
