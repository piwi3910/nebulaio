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
} from '@mantine/core';
import { Dropzone } from '@mantine/dropzone';
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
} from '@tabler/icons-react';
import { consoleApi } from '../api/client';
import { useAuthStore } from '../stores/auth';

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

  const prefix = path || '';

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['bucket-objects', bucketName, prefix],
    queryFn: () =>
      consoleApi.listBucketObjects(bucketName!, { prefix, delimiter: '/' }).then((res) => res.data),
  });

  const listing = data as ListObjectsResponse | undefined;

  const uploadMutation = useMutation({
    mutationFn: (files: File[]) =>
      Promise.all(files.map((file) => consoleApi.uploadObject(bucketName!, file, prefix))),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['bucket-objects', bucketName] });
      setUploadModalOpen(false);
      notifications.show({
        title: 'Upload complete',
        message: 'Files have been uploaded successfully',
        color: 'green',
      });
    },
    onError: () => {
      notifications.show({
        title: 'Upload failed',
        message: 'Failed to upload files',
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

  const handleDownload = (key: string) => {
    // In a real implementation, this would use presigned URLs
    const url = `/api/v1/console/buckets/${bucketName}/objects/${key}`;
    window.open(url, '_blank');
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
          {canWrite && (
            <Button leftSection={<IconUpload size={16} />} onClick={() => setUploadModalOpen(true)}>
              Upload
            </Button>
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
                <Table.Th style={{ width: 80 }}>Actions</Table.Th>
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
                    <ActionIcon variant="subtle">
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
                    <Menu shadow="md" width={150}>
                      <Menu.Target>
                        <ActionIcon variant="subtle">
                          <IconDotsVertical size={16} />
                        </ActionIcon>
                      </Menu.Target>
                      <Menu.Dropdown>
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
                      This folder is empty
                    </Text>
                  </Table.Td>
                </Table.Tr>
              )}
            </Table.Tbody>
          </Table>
        )}
      </Paper>

      {/* Upload Modal */}
      <Modal
        opened={uploadModalOpen}
        onClose={() => setUploadModalOpen(false)}
        title="Upload Files"
        size="lg"
      >
        <Dropzone
          onDrop={(files) => uploadMutation.mutate(files)}
          loading={uploadMutation.isPending}
          accept={[]}
        >
          <Group justify="center" gap="xl" mih={220} style={{ pointerEvents: 'none' }}>
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
    </div>
  );
}
