import { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Title,
  Breadcrumbs,
  Anchor,
  Table,
  Group,
  Text,
  ActionIcon,
  Button,
  Modal,
  Stack,
  Paper,
  Badge,
  Skeleton,
  Menu,
  TextInput,
  Progress,
} from '@mantine/core';
import { Dropzone, FileWithPath } from '@mantine/dropzone';
import { notifications } from '@mantine/notifications';
import {
  IconFolder,
  IconFile,
  IconUpload,
  IconTrash,
  IconDownload,
  IconDotsVertical,
  IconArrowLeft,
  IconRefresh,
  IconFolderPlus,
  IconEye,
  IconSettings,
} from '@tabler/icons-react';
import { consoleApi } from '../api/client';
import { useAuthStore } from '../stores/auth';
import { FilePreview } from '../components/FilePreview';

interface ObjectSummary {
  key: string;
  size: number;
  last_modified: string;
  content_type: string;
  etag: string;
  is_folder: boolean;
}

interface ListObjectsResponse {
  objects: ObjectSummary[];
  folders: string[];
  prefix: string;
  is_truncated: boolean;
  next_page_token?: string;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

export function BucketBrowserPage() {
  const { bucketName, '*': path } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { user } = useAuthStore();
  const canWrite = user?.role !== 'readonly';

  const [uploadModalOpen, setUploadModalOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [createFolderModalOpen, setCreateFolderModalOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState('');
  const [previewTarget, setPreviewTarget] = useState<ObjectSummary | null>(null);
  const [uploadProgress, setUploadProgress] = useState<Record<string, number>>({});
  const [isUploading, setIsUploading] = useState(false);

  const prefix = path || '';

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['bucket-objects', bucketName, prefix],
    queryFn: () =>
      consoleApi.listBucketObjects(bucketName!, { prefix, delimiter: '/' }).then((res) => res.data),
  });

  const listing = data as ListObjectsResponse | undefined;

  const uploadMutation = useMutation({
    mutationFn: async (files: File[]) => {
      setIsUploading(true);
      const newProgress: Record<string, number> = {};
      files.forEach((f) => {
        newProgress[f.name] = 0;
      });
      setUploadProgress(newProgress);

      const results = await Promise.all(
        files.map((file) =>
          consoleApi.uploadObjectWithProgress(
            bucketName!,
            file,
            prefix || undefined,
            (progress) => {
              setUploadProgress((prev) => ({ ...prev, [file.name]: progress }));
            }
          )
        )
      );
      return results;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-objects', bucketName] });
      setUploadModalOpen(false);
      setUploadProgress({});
      setIsUploading(false);
      notifications.show({
        title: 'Upload complete',
        message: 'Files have been uploaded successfully',
        color: 'green',
      });
    },
    onError: () => {
      setIsUploading(false);
      notifications.show({
        title: 'Upload failed',
        message: 'Failed to upload files',
        color: 'red',
      });
    },
  });

  const createFolderMutation = useMutation({
    mutationFn: (folderName: string) => {
      const folderPath = prefix ? `${prefix}${folderName}` : folderName;
      return consoleApi.createFolder(bucketName!, folderPath);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-objects', bucketName] });
      setCreateFolderModalOpen(false);
      setNewFolderName('');
      notifications.show({
        title: 'Folder created',
        message: 'The folder has been created successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to create folder',
        color: 'red',
      });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (key: string) => consoleApi.deleteObject(bucketName!, key),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-objects', bucketName] });
      setDeleteTarget(null);
      notifications.show({
        title: 'Object deleted',
        message: 'The object has been deleted successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Error',
        message: 'Failed to delete object',
        color: 'red',
      });
    },
  });

  // Build breadcrumb items
  const breadcrumbItems = [
    { title: 'Buckets', path: '/buckets' },
    { title: bucketName!, path: `/buckets/${bucketName}` },
  ];

  if (prefix) {
    const parts = prefix.split('/').filter(Boolean);
    let currentPath = '';
    parts.forEach((part) => {
      currentPath += part + '/';
      breadcrumbItems.push({
        title: part,
        path: `/buckets/${bucketName}/${currentPath}`,
      });
    });
  }

  const handleNavigate = (folderPath: string) => {
    navigate(`/buckets/${bucketName}/${folderPath}`);
  };

  const handleBack = () => {
    if (prefix) {
      const parts = prefix.split('/').filter(Boolean);
      parts.pop();
      const newPath = parts.length > 0 ? parts.join('/') + '/' : '';
      navigate(`/buckets/${bucketName}/${newPath}`);
    } else {
      navigate('/buckets');
    }
  };

  const handleDownload = async (key: string) => {
    try {
      const response = await consoleApi.getPresignedDownloadUrl(bucketName!, key);
      window.open(response.data.url, '_blank');
    } catch {
      // Fallback to direct URL
      window.open(`/api/v1/console/buckets/${bucketName}/objects/${encodeURIComponent(key)}`, '_blank');
    }
  };

  const handlePreview = (obj: ObjectSummary) => {
    setPreviewTarget(obj);
  };

  const handleDrop = (files: FileWithPath[]) => {
    uploadMutation.mutate(files as File[]);
  };

  const isPreviewable = (contentType: string, fileName: string): boolean => {
    const ext = fileName.split('.').pop()?.toLowerCase() || '';
    const previewableTypes = ['image/', 'text/', 'application/json', 'application/pdf', 'application/xml'];
    const previewableExts = [
      'js', 'ts', 'tsx', 'jsx', 'py', 'go', 'rs', 'java', 'c', 'cpp', 'h',
      'css', 'scss', 'html', 'xml', 'yaml', 'yml', 'toml', 'ini', 'sh',
      'sql', 'md', 'txt', 'log', 'json', 'pdf',
    ];
    return (
      previewableTypes.some((t) => contentType.toLowerCase().includes(t)) ||
      previewableExts.includes(ext)
    );
  };

  return (
    <div>
      <Group justify="space-between" mb="md">
        <Group>
          <ActionIcon variant="subtle" size="lg" onClick={handleBack}>
            <IconArrowLeft size={20} />
          </ActionIcon>
          <Title order={2}>{bucketName}</Title>
        </Group>
        <Group>
          <ActionIcon variant="subtle" size="lg" onClick={() => refetch()}>
            <IconRefresh size={20} />
          </ActionIcon>
          <ActionIcon
            variant="subtle"
            size="lg"
            onClick={() => navigate(`/buckets/${bucketName}/settings`)}
          >
            <IconSettings size={20} />
          </ActionIcon>
          {canWrite && (
            <>
              <Button
                variant="light"
                leftSection={<IconFolderPlus size={16} />}
                onClick={() => setCreateFolderModalOpen(true)}
              >
                New Folder
              </Button>
              <Button leftSection={<IconUpload size={16} />} onClick={() => setUploadModalOpen(true)}>
                Upload
              </Button>
            </>
          )}
        </Group>
      </Group>

      <Breadcrumbs mb="md">
        {breadcrumbItems.map((item, index) => (
          <Anchor
            key={item.path}
            onClick={() => navigate(item.path)}
            fw={index === breadcrumbItems.length - 1 ? 500 : 400}
          >
            {item.title}
          </Anchor>
        ))}
      </Breadcrumbs>

      {/* Drag and drop zone */}
      {canWrite ? (
        <Dropzone
          onDrop={handleDrop}
          activateOnClick={false}
          styles={{
            root: {
              border: 'none',
              padding: 0,
              backgroundColor: 'transparent',
            },
          }}
        >
          <Paper withBorder>
            {isLoading ? (
              <Stack p="md">
                <Skeleton height={40} />
                <Skeleton height={40} />
                <Skeleton height={40} />
              </Stack>
            ) : (
              <Table striped highlightOnHover>
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Name</Table.Th>
                    <Table.Th>Size</Table.Th>
                    <Table.Th>Type</Table.Th>
                    <Table.Th>Last Modified</Table.Th>
                    <Table.Th style={{ width: 100 }}>Actions</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {/* Folders */}
                  {listing?.folders?.map((folder) => (
                    <Table.Tr
                      key={folder}
                      style={{ cursor: 'pointer' }}
                      onClick={() => handleNavigate(folder)}
                    >
                      <Table.Td>
                        <Group gap="xs">
                          <IconFolder size={18} color="var(--mantine-color-blue-6)" />
                          <Text fw={500}>{folder.replace(prefix, '').replace(/\/$/, '')}</Text>
                        </Group>
                      </Table.Td>
                      <Table.Td>-</Table.Td>
                      <Table.Td>
                        <Badge variant="light" size="sm">
                          Folder
                        </Badge>
                      </Table.Td>
                      <Table.Td>-</Table.Td>
                      <Table.Td>
                        <ActionIcon variant="subtle" onClick={(e) => e.stopPropagation()}>
                          <IconFolder size={16} />
                        </ActionIcon>
                      </Table.Td>
                    </Table.Tr>
                  ))}

                  {/* Objects */}
                  {listing?.objects?.map((obj) => (
                    <Table.Tr key={obj.key}>
                      <Table.Td>
                        <Group gap="xs">
                          <IconFile size={18} />
                          <Text
                            style={{
                              cursor: isPreviewable(obj.content_type, obj.key) ? 'pointer' : 'default',
                            }}
                            onClick={() =>
                              isPreviewable(obj.content_type, obj.key) && handlePreview(obj)
                            }
                          >
                            {obj.key.replace(prefix, '')}
                          </Text>
                        </Group>
                      </Table.Td>
                      <Table.Td>{formatBytes(obj.size)}</Table.Td>
                      <Table.Td>
                        <Badge variant="light" size="sm">
                          {obj.content_type || 'Unknown'}
                        </Badge>
                      </Table.Td>
                      <Table.Td>{new Date(obj.last_modified).toLocaleString()}</Table.Td>
                      <Table.Td>
                        <Menu shadow="md" width={150}>
                          <Menu.Target>
                            <ActionIcon variant="subtle">
                              <IconDotsVertical size={16} />
                            </ActionIcon>
                          </Menu.Target>
                          <Menu.Dropdown>
                            {isPreviewable(obj.content_type, obj.key) && (
                              <Menu.Item
                                leftSection={<IconEye size={14} />}
                                onClick={() => handlePreview(obj)}
                              >
                                Preview
                              </Menu.Item>
                            )}
                            <Menu.Item
                              leftSection={<IconDownload size={14} />}
                              onClick={() => handleDownload(obj.key)}
                            >
                              Download
                            </Menu.Item>
                            {canWrite && (
                              <>
                                <Menu.Divider />
                                <Menu.Item
                                  color="red"
                                  leftSection={<IconTrash size={14} />}
                                  onClick={() => setDeleteTarget(obj.key)}
                                >
                                  Delete
                                </Menu.Item>
                              </>
                            )}
                          </Menu.Dropdown>
                        </Menu>
                      </Table.Td>
                    </Table.Tr>
                  ))}

                  {/* Empty state */}
                  {!listing?.folders?.length && !listing?.objects?.length && (
                    <Table.Tr>
                      <Table.Td colSpan={5}>
                        <Text c="dimmed" ta="center" py="xl">
                          {canWrite
                            ? 'This folder is empty. Drag and drop files here or click Upload.'
                            : 'This folder is empty'}
                        </Text>
                      </Table.Td>
                    </Table.Tr>
                  )}
                </Table.Tbody>
              </Table>
            )}
          </Paper>
        </Dropzone>
      ) : (
        <Paper withBorder>
          {isLoading ? (
            <Stack p="md">
              <Skeleton height={40} />
              <Skeleton height={40} />
              <Skeleton height={40} />
            </Stack>
          ) : (
            <Table striped highlightOnHover>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>Name</Table.Th>
                  <Table.Th>Size</Table.Th>
                  <Table.Th>Type</Table.Th>
                  <Table.Th>Last Modified</Table.Th>
                  <Table.Th style={{ width: 100 }}>Actions</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {listing?.folders?.map((folder) => (
                  <Table.Tr
                    key={folder}
                    style={{ cursor: 'pointer' }}
                    onClick={() => handleNavigate(folder)}
                  >
                    <Table.Td>
                      <Group gap="xs">
                        <IconFolder size={18} color="var(--mantine-color-blue-6)" />
                        <Text fw={500}>{folder.replace(prefix, '').replace(/\/$/, '')}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>-</Table.Td>
                    <Table.Td>
                      <Badge variant="light" size="sm">
                        Folder
                      </Badge>
                    </Table.Td>
                    <Table.Td>-</Table.Td>
                    <Table.Td>-</Table.Td>
                  </Table.Tr>
                ))}
                {listing?.objects?.map((obj) => (
                  <Table.Tr key={obj.key}>
                    <Table.Td>
                      <Group gap="xs">
                        <IconFile size={18} />
                        <Text>{obj.key.replace(prefix, '')}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td>{formatBytes(obj.size)}</Table.Td>
                    <Table.Td>
                      <Badge variant="light" size="sm">
                        {obj.content_type || 'Unknown'}
                      </Badge>
                    </Table.Td>
                    <Table.Td>{new Date(obj.last_modified).toLocaleString()}</Table.Td>
                    <Table.Td>
                      <ActionIcon variant="subtle" onClick={() => handleDownload(obj.key)}>
                        <IconDownload size={16} />
                      </ActionIcon>
                    </Table.Td>
                  </Table.Tr>
                ))}
                {!listing?.folders?.length && !listing?.objects?.length && (
                  <Table.Tr>
                    <Table.Td colSpan={5}>
                      <Text c="dimmed" ta="center" py="xl">
                        This folder is empty
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                )}
              </Table.Tbody>
            </Table>
          )}
        </Paper>
      )}

      {/* Upload Modal */}
      <Modal
        opened={uploadModalOpen}
        onClose={() => !isUploading && setUploadModalOpen(false)}
        title="Upload Files"
        size="lg"
        closeOnClickOutside={!isUploading}
        closeOnEscape={!isUploading}
      >
        <Stack>
          <Dropzone
            onDrop={(files) => uploadMutation.mutate(files)}
            loading={uploadMutation.isPending}
            disabled={isUploading}
          >
            <Group justify="center" gap="xl" mih={180} style={{ pointerEvents: 'none' }}>
              <Dropzone.Accept>
                <IconUpload size={52} color="var(--mantine-color-blue-6)" stroke={1.5} />
              </Dropzone.Accept>
              <Dropzone.Reject>
                <IconFile size={52} color="var(--mantine-color-red-6)" stroke={1.5} />
              </Dropzone.Reject>
              <Dropzone.Idle>
                <IconUpload size={52} color="var(--mantine-color-dimmed)" stroke={1.5} />
              </Dropzone.Idle>

              <div>
                <Text size="xl" inline>
                  Drag files here or click to select
                </Text>
                <Text size="sm" c="dimmed" inline mt={7}>
                  Files will be uploaded to: {prefix || '/'}
                </Text>
              </div>
            </Group>
          </Dropzone>

          {/* Upload Progress */}
          {Object.keys(uploadProgress).length > 0 && (
            <Stack gap="xs">
              <Text size="sm" fw={500}>
                Upload Progress
              </Text>
              {Object.entries(uploadProgress).map(([fileName, progress]) => (
                <div key={fileName}>
                  <Group justify="space-between" mb={4}>
                    <Text size="xs" lineClamp={1} style={{ maxWidth: 300 }}>
                      {fileName}
                    </Text>
                    <Text size="xs" c="dimmed">
                      {progress}%
                    </Text>
                  </Group>
                  <Progress value={progress} size="sm" />
                </div>
              ))}
            </Stack>
          )}
        </Stack>
      </Modal>

      {/* Create Folder Modal */}
      <Modal
        opened={createFolderModalOpen}
        onClose={() => setCreateFolderModalOpen(false)}
        title="Create Folder"
      >
        <Stack>
          <TextInput
            label="Folder Name"
            placeholder="my-folder"
            value={newFolderName}
            onChange={(e) => setNewFolderName(e.target.value)}
            description={`Will be created at: ${prefix || '/'}${newFolderName || ''}/`}
          />
          <Group justify="flex-end">
            <Button variant="default" onClick={() => setCreateFolderModalOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => createFolderMutation.mutate(newFolderName)}
              loading={createFolderMutation.isPending}
              disabled={!newFolderName.trim()}
            >
              Create
            </Button>
          </Group>
        </Stack>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        opened={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Object"
      >
        <Text mb="md">
          Are you sure you want to delete <strong>{deleteTarget}</strong>? This action cannot be
          undone.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={() => setDeleteTarget(null)}>
            Cancel
          </Button>
          <Button
            color="red"
            onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget)}
            loading={deleteMutation.isPending}
          >
            Delete
          </Button>
        </Group>
      </Modal>

      {/* File Preview Modal */}
      {previewTarget && (
        <FilePreview
          bucket={bucketName!}
          objectKey={previewTarget.key}
          contentType={previewTarget.content_type}
          size={previewTarget.size}
          lastModified={previewTarget.last_modified}
          etag={previewTarget.etag}
          opened={previewTarget !== null}
          onClose={() => setPreviewTarget(null)}
        />
      )}
    </div>
  );
}
