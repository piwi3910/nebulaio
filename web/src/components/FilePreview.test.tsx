import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, userEvent } from '../test/utils';
import { FilePreview } from './FilePreview';

// Mock the console API
vi.mock('../api/client', () => ({
  consoleApi: {
    getPresignedUrl: vi.fn().mockResolvedValue({ data: { url: 'https://example.com/test' } }),
    getPresignedDownloadUrl: vi.fn().mockResolvedValue({ data: { url: 'https://example.com/download' } }),
    getObjectContent: vi.fn().mockResolvedValue({ data: { text: () => Promise.resolve('test content') } }),
  },
}));

import { consoleApi } from '../api/client';

const mockConsoleApi = vi.mocked(consoleApi);

describe('FilePreview', () => {
  const defaultProps = {
    bucket: 'test-bucket',
    objectKey: 'path/to/file.txt',
    contentType: 'text/plain',
    size: 1024,
    lastModified: '2024-01-15T12:00:00Z',
    etag: '"abc123"',
    opened: true,
    onClose: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockConsoleApi.getObjectContent.mockResolvedValue({
      data: { text: () => Promise.resolve('test content') },
    } as any);
  });

  it('renders modal when opened', () => {
    render(<FilePreview {...defaultProps} />);
    expect(screen.getByText('file.txt')).toBeInTheDocument();
  });

  it('shows file name from object key', () => {
    render(<FilePreview {...defaultProps} />);
    expect(screen.getByText('file.txt')).toBeInTheDocument();
  });

  it('displays preview and metadata tabs', () => {
    render(<FilePreview {...defaultProps} />);
    expect(screen.getByText('Preview')).toBeInTheDocument();
    expect(screen.getByText('Metadata')).toBeInTheDocument();
  });

  it('shows file extension badge', () => {
    render(<FilePreview {...defaultProps} />);
    expect(screen.getByText('TXT')).toBeInTheDocument();
  });

  it('shows file size badge', () => {
    render(<FilePreview {...defaultProps} />);
    // Use getAllByText since size may appear multiple times
    const sizeElements = screen.getAllByText('1 KB');
    expect(sizeElements.length).toBeGreaterThan(0);
  });

  it('shows content type badge', () => {
    render(<FilePreview {...defaultProps} />);
    // Use getAllByText since content type may appear multiple times
    const contentTypeElements = screen.getAllByText('text/plain');
    expect(contentTypeElements.length).toBeGreaterThan(0);
  });

  it('shows download button', () => {
    render(<FilePreview {...defaultProps} />);
    expect(screen.getByRole('button', { name: /download/i })).toBeInTheDocument();
  });

  it('does not render when closed', () => {
    render(<FilePreview {...defaultProps} opened={false} />);
    expect(screen.queryByText('file.txt')).not.toBeInTheDocument();
  });
});

describe('FilePreview metadata tab', () => {
  const defaultProps = {
    bucket: 'test-bucket',
    objectKey: 'folder/document.pdf',
    contentType: 'application/pdf',
    size: 5242880,
    lastModified: '2024-01-15T12:00:00Z',
    etag: '"etag123"',
    opened: true,
    onClose: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    mockConsoleApi.getPresignedUrl.mockResolvedValue({
      data: { url: 'https://example.com/presigned-url' },
    } as any);
  });

  it('displays object key in metadata tab', async () => {
    const user = userEvent.setup();
    render(<FilePreview {...defaultProps} />);

    await user.click(screen.getByText('Metadata'));

    await waitFor(() => {
      expect(screen.getByText('folder/document.pdf')).toBeInTheDocument();
    });
  });

  it('displays bucket name in metadata tab', async () => {
    const user = userEvent.setup();
    render(<FilePreview {...defaultProps} />);

    await user.click(screen.getByText('Metadata'));

    await waitFor(() => {
      expect(screen.getByText('test-bucket')).toBeInTheDocument();
    });
  });

  it('displays content type in metadata tab', async () => {
    const user = userEvent.setup();
    render(<FilePreview {...defaultProps} />);

    await user.click(screen.getByText('Metadata'));

    await waitFor(() => {
      const contentTypeElements = screen.getAllByText('application/pdf');
      expect(contentTypeElements.length).toBeGreaterThan(0);
    });
  });

  it('displays etag in metadata tab', async () => {
    const user = userEvent.setup();
    render(<FilePreview {...defaultProps} />);

    await user.click(screen.getByText('Metadata'));

    await waitFor(() => {
      expect(screen.getByText('"etag123"')).toBeInTheDocument();
    });
  });
});

describe('FilePreview content type detection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConsoleApi.getPresignedUrl.mockResolvedValue({
      data: { url: 'https://example.com/presigned' },
    } as any);
  });

  it('detects image files', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="photo.jpg"
        contentType="image/jpeg"
        opened={true}
        onClose={vi.fn()}
      />
    );
    // File name may appear multiple times (title + metadata)
    expect(screen.getAllByText('photo.jpg').length).toBeGreaterThan(0);
    expect(screen.getByText('JPG')).toBeInTheDocument();
  });

  it('detects JSON files', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="data.json"
        contentType="application/json"
        opened={true}
        onClose={vi.fn()}
      />
    );
    expect(screen.getAllByText('data.json').length).toBeGreaterThan(0);
    expect(screen.getByText('JSON')).toBeInTheDocument();
  });

  it('detects video files', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="video.mp4"
        contentType="video/mp4"
        opened={true}
        onClose={vi.fn()}
      />
    );
    expect(screen.getAllByText('video.mp4').length).toBeGreaterThan(0);
  });

  it('detects audio files', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="song.mp3"
        contentType="audio/mpeg"
        opened={true}
        onClose={vi.fn()}
      />
    );
    expect(screen.getAllByText('song.mp3').length).toBeGreaterThan(0);
  });

  it('detects PDF files', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="document.pdf"
        contentType="application/pdf"
        opened={true}
        onClose={vi.fn()}
      />
    );
    expect(screen.getAllByText('document.pdf').length).toBeGreaterThan(0);
    expect(screen.getByText('PDF')).toBeInTheDocument();
  });

  it('handles unsupported file types', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="archive.zip"
        contentType="application/zip"
        opened={true}
        onClose={vi.fn()}
      />
    );
    expect(screen.getAllByText('archive.zip').length).toBeGreaterThan(0);
  });
});

describe('FilePreview error handling', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows error when file is too large to preview', async () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="large.txt"
        contentType="text/plain"
        size={20 * 1024 * 1024} // 20MB
        opened={true}
        onClose={vi.fn()}
      />
    );

    await waitFor(() => {
      expect(screen.getByText(/too large to preview/i)).toBeInTheDocument();
    });
  });
});

describe('FilePreview download', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConsoleApi.getPresignedDownloadUrl.mockResolvedValue({
      data: { url: 'https://example.com/download' },
    } as any);
  });

  it('calls download API when button is clicked', async () => {
    const user = userEvent.setup();
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);

    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="file.txt"
        contentType="text/plain"
        opened={true}
        onClose={vi.fn()}
      />
    );

    const downloadButton = screen.getByRole('button', { name: /download/i });
    await user.click(downloadButton);

    await waitFor(() => {
      expect(mockConsoleApi.getPresignedDownloadUrl).toHaveBeenCalledWith('test-bucket', 'file.txt');
    });

    openSpy.mockRestore();
  });
});

describe('FilePreview formatBytes utility', () => {
  it('formats 0 bytes', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="empty.txt"
        contentType="text/plain"
        size={0}
        opened={true}
        onClose={vi.fn()}
      />
    );
    const sizeElements = screen.getAllByText('0 B');
    expect(sizeElements.length).toBeGreaterThan(0);
  });

  it('formats bytes', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="small.txt"
        contentType="text/plain"
        size={512}
        opened={true}
        onClose={vi.fn()}
      />
    );
    const sizeElements = screen.getAllByText('512 B');
    expect(sizeElements.length).toBeGreaterThan(0);
  });

  it('formats kilobytes', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="medium.txt"
        contentType="text/plain"
        size={2048}
        opened={true}
        onClose={vi.fn()}
      />
    );
    const sizeElements = screen.getAllByText('2 KB');
    expect(sizeElements.length).toBeGreaterThan(0);
  });

  it('formats megabytes', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="large.txt"
        contentType="text/plain"
        size={1048576}
        opened={true}
        onClose={vi.fn()}
      />
    );
    const sizeElements = screen.getAllByText('1 MB');
    expect(sizeElements.length).toBeGreaterThan(0);
  });

  it('formats gigabytes', () => {
    render(
      <FilePreview
        bucket="test-bucket"
        objectKey="huge.txt"
        contentType="text/plain"
        size={1073741824}
        opened={true}
        onClose={vi.fn()}
      />
    );
    const sizeElements = screen.getAllByText('1 GB');
    expect(sizeElements.length).toBeGreaterThan(0);
  });
});
