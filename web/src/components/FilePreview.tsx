import { useState, useEffect } from 'react';
import {
  Modal,
  Stack,
  Text,
  Group,
  Badge,
  Loader,
  Center,
  Code,
  ScrollArea,
  Image,
  Paper,
  Tabs,
  Button,
  Alert,
  Table,
} from '@mantine/core';
import {
  IconFile,
  IconPhoto,
  IconCode,
  IconFileText,
  IconDownload,
  IconAlertCircle,
  IconInfoCircle,
  IconFileTypePdf,
  IconVideo,
  IconMusic,
} from '@tabler/icons-react';
import { consoleApi } from '../api/client';

interface FilePreviewProps {
  bucket: string;
  objectKey: string;
  contentType?: string;
  size?: number;
  lastModified?: string;
  etag?: string;
  opened: boolean;
  onClose: () => void;
}

type PreviewType = 'image' | 'text' | 'json' | 'code' | 'pdf' | 'video' | 'audio' | 'unsupported';

function getPreviewType(contentType: string, fileName: string): PreviewType {
  const lowerContentType = contentType.toLowerCase();
  const ext = fileName.split('.').pop()?.toLowerCase() || '';

  // Image types
  const imageExtensions = ['jpg', 'jpeg', 'png', 'gif', 'svg', 'webp', 'bmp', 'ico'];
  if (lowerContentType.startsWith('image/') || imageExtensions.includes(ext)) {
    return 'image';
  }

  // Video types
  const videoExtensions = ['mp4', 'webm', 'ogg', 'mov', 'avi', 'mkv'];
  if (lowerContentType.startsWith('video/') || videoExtensions.includes(ext)) {
    return 'video';
  }

  // Audio types
  const audioExtensions = ['mp3', 'wav', 'ogg', 'aac', 'm4a', 'flac'];
  if (lowerContentType.startsWith('audio/') || audioExtensions.includes(ext)) {
    return 'audio';
  }

  // JSON
  if (lowerContentType === 'application/json' || ext === 'json') {
    return 'json';
  }

  // PDF
  if (lowerContentType === 'application/pdf' || ext === 'pdf') {
    return 'pdf';
  }

  // Text types
  const textExtensions = ['txt', 'md', 'markdown', 'csv', 'log', 'xml', 'yaml', 'yml', 'toml', 'ini', 'env', 'conf', 'config'];
  const textContentTypes = ['text/', 'application/xml', 'application/x-yaml'];
  if (textExtensions.includes(ext) || textContentTypes.some((t) => lowerContentType.includes(t))) {
    return 'text';
  }

  // Code types
  const codeExtensions = [
    'js', 'ts', 'tsx', 'jsx', 'py', 'go', 'rs', 'java', 'c', 'cpp', 'h', 'hpp',
    'css', 'scss', 'sass', 'less', 'html', 'sh', 'bash', 'zsh', 'fish', 'sql',
    'gitignore', 'dockerfile', 'makefile', 'rb', 'php', 'swift', 'kt', 'cs',
    'vb', 'r', 'pl', 'lua', 'vim', 'asm', 'dart', 'scala', 'clj', 'ex', 'exs',
  ];
  const codeContentTypes = ['application/javascript', 'application/typescript', 'application/x-python'];
  if (codeExtensions.includes(ext) || codeContentTypes.some((t) => lowerContentType.includes(t))) {
    return 'code';
  }

  return 'unsupported';
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function getFileExtension(fileName: string): string {
  return fileName.split('.').pop()?.toUpperCase() || 'FILE';
}

export function FilePreview({
  bucket,
  objectKey,
  contentType = 'application/octet-stream',
  size = 0,
  lastModified,
  etag,
  opened,
  onClose,
}: FilePreviewProps) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [content, setContent] = useState<string | null>(null);
  const [imageUrl, setImageUrl] = useState<string | null>(null);

  const fileName = objectKey.split('/').pop() || objectKey;
  const previewType = getPreviewType(contentType, fileName);

  useEffect(() => {
    if (!opened) {
      setContent(null);
      setImageUrl(null);
      setError(null);
      return;
    }

    // Don't load content for unsupported types
    if (previewType === 'unsupported') {
      return;
    }

    // Limit preview to files under 10MB for text/code/json
    const textBasedTypes = ['text', 'code', 'json'];
    if (textBasedTypes.includes(previewType) && size > 10 * 1024 * 1024) {
      setError('File is too large to preview (max 10MB for text content)');
      return;
    }

    const loadContent = async () => {
      setLoading(true);
      setError(null);

      try {
        if (previewType === 'image' || previewType === 'pdf' || previewType === 'video' || previewType === 'audio') {
          // For media files, get a presigned URL
          const response = await consoleApi.getPresignedUrl(bucket, objectKey);
          setImageUrl(response.data.url);
        } else {
          // For text/code/json, fetch the content
          const response = await consoleApi.getObjectContent(bucket, objectKey);
          const text = await response.data.text();

          if (previewType === 'json') {
            try {
              const parsed = JSON.parse(text);
              setContent(JSON.stringify(parsed, null, 2));
            } catch {
              setContent(text);
            }
          } else {
            setContent(text);
          }
        }
      } catch (err) {
        console.error('Failed to load preview:', err);
        setError('Failed to load file preview');
      } finally {
        setLoading(false);
      }
    };

    loadContent();
  }, [opened, bucket, objectKey, previewType, size]);

  const handleDownload = async () => {
    try {
      const response = await consoleApi.getPresignedDownloadUrl(bucket, objectKey);
      window.open(response.data.url, '_blank');
    } catch {
      // Fallback to direct download
      window.open(`/api/v1/console/buckets/${bucket}/objects/${encodeURIComponent(objectKey)}`, '_blank');
    }
  };

  const getPreviewIcon = () => {
    switch (previewType) {
      case 'image':
        return <IconPhoto size={20} />;
      case 'video':
        return <IconVideo size={20} />;
      case 'audio':
        return <IconMusic size={20} />;
      case 'json':
      case 'code':
        return <IconCode size={20} />;
      case 'text':
        return <IconFileText size={20} />;
      case 'pdf':
        return <IconFileTypePdf size={20} />;
      default:
        return <IconFile size={20} />;
    }
  };

  const renderPreview = () => {
    if (loading) {
      return (
        <Center h={300}>
          <Loader size="lg" />
        </Center>
      );
    }

    if (error) {
      return (
        <Alert icon={<IconAlertCircle size={16} />} color="red" variant="light">
          {error}
        </Alert>
      );
    }

    switch (previewType) {
      case 'image':
        if (!imageUrl) {
          return (
            <Center h={300}>
              <Loader size="lg" />
            </Center>
          );
        }
        return (
          <Center>
            <Image
              src={imageUrl}
              alt={fileName}
              maw="100%"
              mah={500}
              fit="contain"
              radius="md"
            />
          </Center>
        );

      case 'video':
        if (!imageUrl) {
          return (
            <Center h={300}>
              <Loader size="lg" />
            </Center>
          );
        }
        return (
          <Center>
            <video
              src={imageUrl}
              controls
              style={{
                maxWidth: '100%',
                maxHeight: '500px',
                borderRadius: '8px',
              }}
            >
              Your browser does not support the video tag.
            </video>
          </Center>
        );

      case 'audio':
        if (!imageUrl) {
          return (
            <Center h={300}>
              <Loader size="lg" />
            </Center>
          );
        }
        return (
          <Paper withBorder p="xl" ta="center">
            <IconMusic size={48} color="gray" style={{ marginBottom: '1rem' }} />
            <Text size="lg" fw={500} mb="md">
              {fileName}
            </Text>
            <audio
              src={imageUrl}
              controls
              style={{
                width: '100%',
                maxWidth: '500px',
              }}
            >
              Your browser does not support the audio tag.
            </audio>
          </Paper>
        );

      case 'pdf':
        if (!imageUrl) {
          return (
            <Center h={300}>
              <Loader size="lg" />
            </Center>
          );
        }
        return (
          <iframe
            src={imageUrl}
            style={{ width: '100%', height: '500px', border: 'none' }}
            title={fileName}
          />
        );

      case 'json':
        if (!content) {
          return (
            <Center h={300}>
              <Loader size="lg" />
            </Center>
          );
        }
        return (
          <ScrollArea h={400}>
            <Code block style={{ whiteSpace: 'pre-wrap', fontSize: '13px' }}>
              {content}
            </Code>
          </ScrollArea>
        );

      case 'code':
      case 'text':
        if (!content) {
          return (
            <Center h={300}>
              <Loader size="lg" />
            </Center>
          );
        }
        return (
          <ScrollArea h={400}>
            <Code block style={{ whiteSpace: 'pre-wrap', fontSize: '13px' }}>
              {content}
            </Code>
          </ScrollArea>
        );

      case 'unsupported':
        return (
          <Paper withBorder p="xl" ta="center">
            <IconFile size={48} color="gray" style={{ marginBottom: '1rem' }} />
            <Text size="lg" fw={500} mb="xs">
              Preview not available
            </Text>
            <Text c="dimmed" size="sm" mb="md">
              This file type cannot be previewed. Click download to view the file.
            </Text>
            <Button leftSection={<IconDownload size={16} />} onClick={handleDownload}>
              Download File
            </Button>
          </Paper>
        );

      default:
        return (
          <Paper withBorder p="xl" ta="center">
            <IconFile size={48} color="gray" style={{ marginBottom: '1rem' }} />
            <Text size="lg" fw={500} mb="xs">
              Preview not available
            </Text>
            <Text c="dimmed" size="sm" mb="md">
              This file type cannot be previewed. Click download to view the file.
            </Text>
            <Button leftSection={<IconDownload size={16} />} onClick={handleDownload}>
              Download File
            </Button>
          </Paper>
        );
    }
  };

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="xs">
          {getPreviewIcon()}
          <Text fw={500} lineClamp={1} style={{ maxWidth: 400 }}>
            {fileName}
          </Text>
        </Group>
      }
      size="xl"
    >
      <Tabs defaultValue="preview">
        <Tabs.List>
          <Tabs.Tab value="preview" leftSection={getPreviewIcon()}>
            Preview
          </Tabs.Tab>
          <Tabs.Tab value="metadata" leftSection={<IconInfoCircle size={16} />}>
            Metadata
          </Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="preview" pt="md">
          <Stack>
            <Group justify="space-between">
              <Group gap="xs">
                <Badge variant="light" size="sm">
                  {getFileExtension(fileName)}
                </Badge>
                <Badge variant="light" size="sm" color="gray">
                  {formatBytes(size)}
                </Badge>
                <Badge variant="light" size="sm" color="gray">
                  {contentType}
                </Badge>
              </Group>
              <Button
                variant="light"
                size="xs"
                leftSection={<IconDownload size={14} />}
                onClick={handleDownload}
              >
                Download
              </Button>
            </Group>

            {renderPreview()}
          </Stack>
        </Tabs.Panel>

        <Tabs.Panel value="metadata" pt="md">
          <Table>
            <Table.Tbody>
              <Table.Tr>
                <Table.Td fw={500} w={150}>
                  Key
                </Table.Td>
                <Table.Td>
                  <Code>{objectKey}</Code>
                </Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td fw={500}>Bucket</Table.Td>
                <Table.Td>{bucket}</Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td fw={500}>Size</Table.Td>
                <Table.Td>{formatBytes(size)}</Table.Td>
              </Table.Tr>
              <Table.Tr>
                <Table.Td fw={500}>Content Type</Table.Td>
                <Table.Td>{contentType}</Table.Td>
              </Table.Tr>
              {lastModified && (
                <Table.Tr>
                  <Table.Td fw={500}>Last Modified</Table.Td>
                  <Table.Td>{new Date(lastModified).toLocaleString()}</Table.Td>
                </Table.Tr>
              )}
              {etag && (
                <Table.Tr>
                  <Table.Td fw={500}>ETag</Table.Td>
                  <Table.Td>
                    <Code>{etag}</Code>
                  </Table.Td>
                </Table.Tr>
              )}
            </Table.Tbody>
          </Table>
        </Tabs.Panel>
      </Tabs>
    </Modal>
  );
}
