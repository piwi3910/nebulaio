import { useState, useCallback } from 'react';
import {
  Stack,
  Text,
  Group,
  Progress,
  Paper,
  ActionIcon,
  Badge,
  Button,
  rem,
} from '@mantine/core';
import { Dropzone, FileWithPath, FileRejection } from '@mantine/dropzone';
import { notifications } from '@mantine/notifications';
import {
  IconUpload,
  IconX,
  IconFile,
  IconCheck,
  IconAlertCircle,
  IconTrash,
  IconCloudUpload,
} from '@tabler/icons-react';
import { consoleApi } from '../api/client';

interface UploadFile {
  id: string;
  file: FileWithPath;
  progress: number;
  status: 'pending' | 'uploading' | 'completed' | 'error';
  error?: string;
}

interface FileUploaderProps {
  bucket: string;
  prefix?: string;
  onUploadComplete?: () => void;
  maxFiles?: number;
  maxSize?: number; // in bytes
  accept?: string[];
}

export function FileUploader({
  bucket,
  prefix = '',
  onUploadComplete,
  maxFiles = 10,
  maxSize = 5 * 1024 * 1024 * 1024, // 5GB default
  accept,
}: FileUploaderProps) {
  const [files, setFiles] = useState<UploadFile[]>([]);
  const [isUploading, setIsUploading] = useState(false);

  const handleDrop = useCallback((acceptedFiles: FileWithPath[]) => {
    const newFiles: UploadFile[] = acceptedFiles.map((file) => ({
      id: `${file.name}-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
      file,
      progress: 0,
      status: 'pending' as const,
    }));

    setFiles((prev) => [...prev, ...newFiles].slice(0, maxFiles));
  }, [maxFiles]);

  const handleReject = useCallback((rejectedFiles: FileRejection[]) => {
    rejectedFiles.forEach(({ file, errors }) => {
      notifications.show({
        title: 'File rejected',
        message: `${file.name}: ${errors.map((e) => e.message).join(', ')}`,
        color: 'red',
      });
    });
  }, []);

  const removeFile = useCallback((id: string) => {
    setFiles((prev) => prev.filter((f) => f.id !== id));
  }, []);

  const uploadFile = async (uploadFile: UploadFile): Promise<void> => {
    const path = prefix ? `${prefix}${uploadFile.file.name}` : uploadFile.file.name;

    try {
      setFiles((prev) =>
        prev.map((f) =>
          f.id === uploadFile.id ? { ...f, status: 'uploading' as const } : f
        )
      );

      await consoleApi.uploadObjectWithProgress(
        bucket,
        uploadFile.file,
        path,
        (progress) => {
          setFiles((prev) =>
            prev.map((f) =>
              f.id === uploadFile.id ? { ...f, progress } : f
            )
          );
        }
      );

      setFiles((prev) =>
        prev.map((f) =>
          f.id === uploadFile.id
            ? { ...f, status: 'completed' as const, progress: 100 }
            : f
        )
      );
    } catch (error) {
      setFiles((prev) =>
        prev.map((f) =>
          f.id === uploadFile.id
            ? {
                ...f,
                status: 'error' as const,
                error: error instanceof Error ? error.message : 'Upload failed',
              }
            : f
        )
      );
    }
  };

  const uploadAllFiles = async () => {
    const pendingFiles = files.filter((f) => f.status === 'pending');
    if (pendingFiles.length === 0) return;

    setIsUploading(true);

    // Upload files sequentially to avoid overwhelming the server
    for (const file of pendingFiles) {
      await uploadFile(file);
    }

    setIsUploading(false);

    // Use callback to get current state to avoid stale closure
    setFiles((currentFiles) => {
      const successCount = currentFiles.filter((f) => f.status === 'completed').length;
      const errorCount = currentFiles.filter((f) => f.status === 'error').length;

      if (errorCount === 0) {
        notifications.show({
          title: 'Upload complete',
          message: `Successfully uploaded ${successCount} file(s)`,
          color: 'green',
        });
        onUploadComplete?.();
      } else {
        notifications.show({
          title: 'Upload partially complete',
          message: `${successCount} succeeded, ${errorCount} failed`,
          color: 'yellow',
        });
      }

      return currentFiles;
    });
  };

  const clearCompleted = () => {
    setFiles((prev) => prev.filter((f) => f.status !== 'completed'));
  };

  const formatFileSize = (bytes: number): string => {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
  };

  const getStatusColor = (status: UploadFile['status']): string => {
    switch (status) {
      case 'completed':
        return 'green';
      case 'error':
        return 'red';
      case 'uploading':
        return 'blue';
      default:
        return 'gray';
    }
  };

  const getStatusIcon = (status: UploadFile['status']) => {
    switch (status) {
      case 'completed':
        return <IconCheck size={16} />;
      case 'error':
        return <IconAlertCircle size={16} />;
      default:
        return <IconFile size={16} />;
    }
  };

  const pendingCount = files.filter((f) => f.status === 'pending').length;
  const completedCount = files.filter((f) => f.status === 'completed').length;

  return (
    <Stack gap="md">
      <Dropzone
        onDrop={handleDrop}
        onReject={handleReject}
        maxSize={maxSize}
        maxFiles={maxFiles}
        accept={accept ? accept : undefined}
        disabled={isUploading}
        styles={{
          root: {
            borderWidth: 2,
            borderStyle: 'dashed',
          },
        }}
      >
        <Group justify="center" gap="xl" mih={120} style={{ pointerEvents: 'none' }}>
          <Dropzone.Accept>
            <IconUpload
              style={{ width: rem(52), height: rem(52), color: 'var(--mantine-color-blue-6)' }}
              stroke={1.5}
            />
          </Dropzone.Accept>
          <Dropzone.Reject>
            <IconX
              style={{ width: rem(52), height: rem(52), color: 'var(--mantine-color-red-6)' }}
              stroke={1.5}
            />
          </Dropzone.Reject>
          <Dropzone.Idle>
            <IconCloudUpload
              style={{ width: rem(52), height: rem(52), color: 'var(--mantine-color-dimmed)' }}
              stroke={1.5}
            />
          </Dropzone.Idle>

          <div>
            <Text size="xl" inline>
              Drag files here or click to select
            </Text>
            <Text size="sm" c="dimmed" inline mt={7}>
              Upload up to {maxFiles} files, each up to {formatFileSize(maxSize)}
            </Text>
          </div>
        </Group>
      </Dropzone>

      {files.length > 0 && (
        <>
          <Group justify="space-between">
            <Text size="sm" c="dimmed">
              {files.length} file(s) selected
              {pendingCount > 0 && ` (${pendingCount} pending)`}
              {completedCount > 0 && ` (${completedCount} completed)`}
            </Text>
            <Group gap="xs">
              {completedCount > 0 && (
                <Button
                  variant="subtle"
                  size="xs"
                  onClick={clearCompleted}
                  leftSection={<IconTrash size={14} />}
                >
                  Clear completed
                </Button>
              )}
              {pendingCount > 0 && (
                <Button
                  size="xs"
                  onClick={uploadAllFiles}
                  loading={isUploading}
                  leftSection={<IconUpload size={14} />}
                >
                  Upload {pendingCount} file(s)
                </Button>
              )}
            </Group>
          </Group>

          <Stack gap="xs">
            {files.map((file) => (
              <Paper key={file.id} withBorder p="sm">
                <Group justify="space-between" mb={file.status === 'uploading' ? 'xs' : 0}>
                  <Group gap="sm">
                    {getStatusIcon(file.status)}
                    <div>
                      <Text size="sm" fw={500} lineClamp={1}>
                        {file.file.name}
                      </Text>
                      <Text size="xs" c="dimmed">
                        {formatFileSize(file.file.size)}
                      </Text>
                    </div>
                  </Group>

                  <Group gap="xs">
                    <Badge color={getStatusColor(file.status)} size="sm">
                      {file.status === 'uploading'
                        ? `${file.progress}%`
                        : file.status}
                    </Badge>
                    {(file.status === 'pending' || file.status === 'error') && (
                      <ActionIcon
                        variant="subtle"
                        color="red"
                        size="sm"
                        onClick={() => removeFile(file.id)}
                      >
                        <IconX size={14} />
                      </ActionIcon>
                    )}
                  </Group>
                </Group>

                {file.status === 'uploading' && (
                  <Progress value={file.progress} size="sm" animated />
                )}

                {file.status === 'error' && file.error && (
                  <Text size="xs" c="red" mt="xs">
                    {file.error}
                  </Text>
                )}
              </Paper>
            ))}
          </Stack>
        </>
      )}
    </Stack>
  );
}
