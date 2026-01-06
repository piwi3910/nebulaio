import { useState, useCallback, useRef, useEffect, Component, ReactNode } from 'react';
import SwaggerUI from 'swagger-ui-react';
import 'swagger-ui-react/swagger-ui.css';

// Error boundary for Swagger UI component
interface SwaggerErrorBoundaryProps {
  children: ReactNode;
}

interface SwaggerErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class SwaggerErrorBoundary extends Component<SwaggerErrorBoundaryProps, SwaggerErrorBoundaryState> {
  constructor(props: SwaggerErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): SwaggerErrorBoundaryState {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div style={{ padding: '20px', textAlign: 'center' }}>
          <h3>Failed to load API documentation</h3>
          <p style={{ color: '#666' }}>
            {this.state.error?.message || 'An unexpected error occurred'}
          </p>
          <button
            onClick={() => this.setState({ hasError: false, error: null })}
            style={{
              padding: '8px 16px',
              backgroundColor: '#228be6',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer',
            }}
          >
            Try Again
          </button>
        </div>
      );
    }

    return this.props.children;
  }
}
import {
  Container,
  Title,
  Paper,
  Tabs,
  Stack,
  Group,
  Text,
  Button,
  Card,
  Badge,
  ActionIcon,
  CopyButton,
  Tooltip,
  Code,
  ScrollArea,
  Divider,
  TextInput,
  Alert,
  Modal,
  Table,
  SegmentedControl,
} from '@mantine/core';
import {
  IconApi,
  IconHistory,
  IconCode,
  IconCopy,
  IconCheck,
  IconTrash,
  IconDownload,
  IconInfoCircle,
  IconSearch,
  IconX,
} from '@tabler/icons-react';
import { notifications } from '@mantine/notifications';
import { useAuthStore } from '../stores/auth';
import { apiClient } from '../api/client';

interface RequestHistoryEntry {
  id: string;
  timestamp: string;
  method: string;
  path: string;
  statusCode: number;
  responseTimeMs: number;
}

interface CodeSnippet {
  language: string;
  code: string;
}

interface PendingRequest {
  startTime: number;
  method: string;
  path: string;
}

// Swagger UI interceptor types - using intersection with Swagger's native types
interface SwaggerRequestFields {
  url?: string;
  method?: string;
  headers?: Record<string, string>;
  body?: string;
}

interface SwaggerResponseFields {
  url?: string;
  status?: number;
  headers?: Record<string, string>;
  body?: string;
}

const MAX_HISTORY_SIZE = 50;

// UI dimension constants
const HISTORY_SCROLL_HEIGHT = 500;
const CODE_SNIPPET_SCROLL_HEIGHT = 300;

// Pending request timeout in milliseconds (30 seconds)
// Requests older than this are considered stale and cleaned up to prevent memory leaks
const PENDING_REQUEST_TIMEOUT_MS = 30000;
const PENDING_REQUEST_CLEANUP_INTERVAL_MS = 10000;

// Helper function to load history from localStorage
function loadHistoryFromStorage(): RequestHistoryEntry[] {
  const stored = localStorage.getItem('nebulaio-api-history');
  if (stored) {
    try {
      return JSON.parse(stored) as RequestHistoryEntry[];
    } catch {
      // Invalid JSON in localStorage, reset to empty array
      return [];
    }
  }

  return [];
}

export function ApiExplorerPage() {
  const [activeTab, setActiveTab] = useState<string | null>('explorer');
  const [requestHistory, setRequestHistory] = useState<RequestHistoryEntry[]>(loadHistoryFromStorage);
  const [codeSnippets, setCodeSnippets] = useState<CodeSnippet[]>([]);
  const [selectedLanguage, setSelectedLanguage] = useState('curl');
  const [codeModalOpened, setCodeModalOpened] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const { accessToken } = useAuthStore();

  // Track pending requests by URL to correlate with responses
  const pendingRequestsRef = useRef<Map<string, PendingRequest>>(new Map());

  // Save request history to localStorage
  const saveHistory = useCallback((history: RequestHistoryEntry[]) => {
    localStorage.setItem('nebulaio-api-history', JSON.stringify(history));
  }, []);

  // Add request to history
  const addToHistory = useCallback(
    (entry: Omit<RequestHistoryEntry, 'id'>) => {
      const newEntry: RequestHistoryEntry = {
        ...entry,
        id: `${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
      };

      setRequestHistory((prev) => {
        const updated = [newEntry, ...prev].slice(0, MAX_HISTORY_SIZE);
        saveHistory(updated);

        return updated;
      });
    },
    [saveHistory]
  );

  // Clear history
  const clearHistory = useCallback(() => {
    setRequestHistory([]);
    localStorage.removeItem('nebulaio-api-history');
  }, []);

  // Delete single history entry
  const deleteHistoryEntry = useCallback(
    (id: string) => {
      setRequestHistory((prev) => {
        const updated = prev.filter((entry) => entry.id !== id);
        saveHistory(updated);

        return updated;
      });
    },
    [saveHistory]
  );

  // Generate code snippets
  const generateCodeSnippets = useCallback(
    async (method: string, path: string, body?: string) => {
      try {
        const response = await apiClient.post('/api-explorer/code-snippets', {
          method,
          url: `${window.location.origin}/api/v1${path}`,
          headers: {
            'Content-Type': 'application/json',
            Authorization: accessToken ? `Bearer ${accessToken}` : '',
          },
          body,
        });

        setCodeSnippets(response.data.snippets || []);
        setCodeModalOpened(true);
      } catch (error) {
        // Log error for debugging while showing user-friendly notification
        console.error('Failed to generate code snippets:', error);
        notifications.show({
          title: 'Error',
          message: 'Failed to generate code snippets. Please try again.',
          color: 'red',
          icon: <IconX size={16} />,
        });
      }
    },
    [accessToken]
  );

  // Export OpenAPI spec
  const exportSpec = useCallback(async (format: 'json' | 'yaml') => {
    try {
      const response = await fetch(`/api/v1/openapi.${format}`);
      const content = await response.text();
      const blob = new Blob([content], { type: format === 'json' ? 'application/json' : 'application/x-yaml' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `nebulaio-openapi.${format}`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
    } catch (error) {
      // Log error for debugging while showing user-friendly notification
      console.error(`Failed to export OpenAPI spec as ${format}:`, error);
      notifications.show({
        title: 'Export Failed',
        message: `Failed to export OpenAPI specification as ${format.toUpperCase()}.`,
        color: 'red',
        icon: <IconX size={16} />,
      });
    }
  }, []);

  // Filter history based on search
  const filteredHistory = requestHistory.filter(
    (entry) =>
      entry.path.toLowerCase().includes(searchQuery.toLowerCase()) ||
      entry.method.toLowerCase().includes(searchQuery.toLowerCase())
  );

  // Get status badge color
  const getStatusColor = (statusCode: number) => {
    if (statusCode >= 200 && statusCode < 300) return 'green';
    if (statusCode >= 300 && statusCode < 400) return 'blue';
    if (statusCode >= 400 && statusCode < 500) return 'yellow';

    return 'red';
  };

  // Get method badge color
  const getMethodColor = (method: string) => {
    const colors: Record<string, string> = {
      GET: 'blue',
      POST: 'green',
      PUT: 'orange',
      DELETE: 'red',
      PATCH: 'violet',
    };

    return colors[method.toUpperCase()] || 'gray';
  };

  // Request interceptor for Swagger UI - tracks request start time
  const requestInterceptor = useCallback(<T extends SwaggerRequestFields>(request: T): T => {
    const token = useAuthStore.getState().accessToken;
    if (token && request.headers) {
      request.headers.Authorization = `Bearer ${token}`;
    }

    // Track the request start time for API requests
    if (request.url && request.url.includes('/api/v1/')) {
      try {
        const urlObj = new URL(request.url, window.location.origin);
        const path = urlObj.pathname.replace('/api/v1', '');
        pendingRequestsRef.current.set(request.url, {
          startTime: Date.now(),
          method: request.method || 'GET',
          path,
        });
      } catch {
        // URL parsing errors are expected for malformed URLs; silently ignore
      }
    }

    return request;
  }, []);

  // Response interceptor for Swagger UI - records completed requests
  const responseInterceptor = useCallback(<T extends SwaggerResponseFields>(response: T): T => {
    // Track completed API requests
    if (response.url && response.url.includes('/api/v1/')) {
      const pending = pendingRequestsRef.current.get(response.url);
      if (pending && response.status !== undefined) {
        const endTime = Date.now();
        addToHistory({
          timestamp: new Date().toISOString(),
          method: pending.method,
          path: pending.path,
          statusCode: response.status,
          responseTimeMs: endTime - pending.startTime,
        });
        pendingRequestsRef.current.delete(response.url);
      }
    }

    return response;
  }, [addToHistory]);

  // Cleanup stale pending requests periodically and on unmount
  useEffect(() => {
    const pendingRequests = pendingRequestsRef.current;

    // Periodic cleanup of stale pending requests to prevent memory leaks
    const cleanupInterval = setInterval(() => {
      const now = Date.now();
      const staleUrls: string[] = [];

      pendingRequests.forEach((request, url) => {
        if (now - request.startTime > PENDING_REQUEST_TIMEOUT_MS) {
          staleUrls.push(url);
        }
      });

      staleUrls.forEach((url) => pendingRequests.delete(url));
    }, PENDING_REQUEST_CLEANUP_INTERVAL_MS);

    return () => {
      clearInterval(cleanupInterval);
      pendingRequests.clear();
    };
  }, []);

  return (
    <Container size="xl">
      <Stack gap="lg">
        <Group justify="space-between">
          <Group>
            <IconApi size={32} />
            <div>
              <Title order={2}>API Explorer</Title>
              <Text c="dimmed" size="sm">
                Interactive documentation for NebulaIO Admin and S3 APIs
              </Text>
            </div>
          </Group>

          <Group>
            <Button
              variant="subtle"
              leftSection={<IconDownload size={16} />}
              onClick={() => exportSpec('json')}
            >
              Export JSON
            </Button>
            <Button
              variant="subtle"
              leftSection={<IconDownload size={16} />}
              onClick={() => exportSpec('yaml')}
            >
              Export YAML
            </Button>
          </Group>
        </Group>

        {accessToken ? (
          <Alert icon={<IconCheck size={16} />} color="green" variant="light">
            Authentication token detected. API requests will be authenticated automatically.
          </Alert>
        ) : (
          <Alert icon={<IconInfoCircle size={16} />} color="yellow" variant="light">
            No authentication token. Please log in to test authenticated endpoints.
          </Alert>
        )}

        <Paper shadow="sm" radius="md" withBorder>
          <Tabs value={activeTab} onChange={setActiveTab}>
            <Tabs.List>
              <Tabs.Tab value="explorer" leftSection={<IconApi size={16} />}>
                API Explorer
              </Tabs.Tab>
              <Tabs.Tab value="history" leftSection={<IconHistory size={16} />}>
                Request History
                {requestHistory.length > 0 && (
                  <Badge size="sm" ml={8}>
                    {requestHistory.length}
                  </Badge>
                )}
              </Tabs.Tab>
            </Tabs.List>

            <Tabs.Panel value="explorer" p="md">
              <div className="swagger-ui-wrapper">
                <SwaggerErrorBoundary>
                  <SwaggerUI
                    url="/api/v1/openapi.json"
                    requestInterceptor={requestInterceptor}
                    responseInterceptor={responseInterceptor}
                    docExpansion="list"
                    defaultModelsExpandDepth={1}
                    persistAuthorization={true}
                    tryItOutEnabled={true}
                  />
                </SwaggerErrorBoundary>
              </div>
            </Tabs.Panel>

            <Tabs.Panel value="history" p="md">
              <Stack gap="md">
                <Group justify="space-between">
                  <TextInput
                    placeholder="Search requests..."
                    leftSection={<IconSearch size={16} />}
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.currentTarget.value)}
                    style={{ flex: 1, maxWidth: 400 }}
                  />
                  <Button
                    variant="subtle"
                    color="red"
                    leftSection={<IconTrash size={16} />}
                    onClick={clearHistory}
                    disabled={requestHistory.length === 0}
                  >
                    Clear History
                  </Button>
                </Group>

                {filteredHistory.length === 0 ? (
                  <Card withBorder p="xl">
                    <Stack align="center" gap="md">
                      <IconHistory size={48} opacity={0.5} />
                      <Text c="dimmed">
                        {searchQuery ? 'No matching requests found' : 'No request history yet'}
                      </Text>
                      <Text c="dimmed" size="sm">
                        Try out some API endpoints to see your request history here
                      </Text>
                    </Stack>
                  </Card>
                ) : (
                  <ScrollArea h={HISTORY_SCROLL_HEIGHT}>
                    <Table striped highlightOnHover>
                      <Table.Thead>
                        <Table.Tr>
                          <Table.Th>Time</Table.Th>
                          <Table.Th>Method</Table.Th>
                          <Table.Th>Path</Table.Th>
                          <Table.Th>Status</Table.Th>
                          <Table.Th>Duration</Table.Th>
                          <Table.Th>Actions</Table.Th>
                        </Table.Tr>
                      </Table.Thead>
                      <Table.Tbody>
                        {filteredHistory.map((entry) => (
                          <Table.Tr key={entry.id}>
                            <Table.Td>
                              <Text size="sm" c="dimmed">
                                {new Date(entry.timestamp).toLocaleTimeString()}
                              </Text>
                            </Table.Td>
                            <Table.Td>
                              <Badge color={getMethodColor(entry.method)} variant="light">
                                {entry.method}
                              </Badge>
                            </Table.Td>
                            <Table.Td>
                              <Code>{entry.path}</Code>
                            </Table.Td>
                            <Table.Td>
                              <Badge color={getStatusColor(entry.statusCode)} variant="light">
                                {entry.statusCode}
                              </Badge>
                            </Table.Td>
                            <Table.Td>
                              <Text size="sm">{entry.responseTimeMs}ms</Text>
                            </Table.Td>
                            <Table.Td>
                              <Group gap="xs">
                                <Tooltip label="Generate Code">
                                  <ActionIcon
                                    variant="subtle"
                                    onClick={() => generateCodeSnippets(entry.method, entry.path)}
                                  >
                                    <IconCode size={16} />
                                  </ActionIcon>
                                </Tooltip>
                                <Tooltip label="Delete">
                                  <ActionIcon
                                    variant="subtle"
                                    color="red"
                                    onClick={() => deleteHistoryEntry(entry.id)}
                                  >
                                    <IconTrash size={16} />
                                  </ActionIcon>
                                </Tooltip>
                              </Group>
                            </Table.Td>
                          </Table.Tr>
                        ))}
                      </Table.Tbody>
                    </Table>
                  </ScrollArea>
                )}
              </Stack>
            </Tabs.Panel>
          </Tabs>
        </Paper>
      </Stack>

      {/* Code Generation Modal */}
      <Modal
        opened={codeModalOpened}
        onClose={() => setCodeModalOpened(false)}
        title="Code Snippets"
        size="lg"
      >
        <Stack gap="md">
          <SegmentedControl
            value={selectedLanguage}
            onChange={setSelectedLanguage}
            data={[
              { label: 'cURL', value: 'curl' },
              { label: 'JavaScript', value: 'javascript' },
              { label: 'Python', value: 'python' },
              { label: 'Go', value: 'go' },
            ]}
          />

          <Divider />

          {codeSnippets
            .filter((s) => s.language === selectedLanguage)
            .map((snippet) => (
              <div key={snippet.language}>
                <Group justify="space-between" mb="xs">
                  <Text size="sm" fw={500}>
                    {snippet.language.charAt(0).toUpperCase() + snippet.language.slice(1)}
                  </Text>
                  <CopyButton value={snippet.code}>
                    {({ copied, copy }) => (
                      <Button
                        variant="subtle"
                        size="xs"
                        leftSection={copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
                        onClick={copy}
                      >
                        {copied ? 'Copied' : 'Copy'}
                      </Button>
                    )}
                  </CopyButton>
                </Group>
                <ScrollArea h={CODE_SNIPPET_SCROLL_HEIGHT}>
                  <Code block>{snippet.code}</Code>
                </ScrollArea>
              </div>
            ))}
        </Stack>
      </Modal>

      {/* Custom styles for Swagger UI */}
      <style>{`
        .swagger-ui-wrapper {
          background: var(--mantine-color-body);
        }
        .swagger-ui .topbar {
          display: none;
        }
        .swagger-ui .info {
          margin: 20px 0;
        }
        .swagger-ui .scheme-container {
          background: transparent;
          box-shadow: none;
          padding: 20px 0;
        }
        .swagger-ui .opblock-tag {
          border-bottom: 1px solid var(--mantine-color-default-border);
        }
        .swagger-ui .opblock {
          border-radius: var(--mantine-radius-md);
          margin-bottom: 10px;
        }
        .swagger-ui .btn {
          border-radius: var(--mantine-radius-sm);
        }
        .swagger-ui .try-out__btn {
          border-color: var(--mantine-color-blue-6);
          color: var(--mantine-color-blue-6);
        }
        .swagger-ui .try-out__btn:hover {
          background-color: var(--mantine-color-blue-light);
        }
        .swagger-ui .execute {
          background-color: var(--mantine-color-blue-6);
          border-color: var(--mantine-color-blue-6);
        }
        .swagger-ui .execute:hover {
          background-color: var(--mantine-color-blue-7);
        }
        .swagger-ui .model-box {
          background: var(--mantine-color-default);
        }

        /* Dark mode support */
        [data-mantine-color-scheme="dark"] .swagger-ui {
          filter: invert(88%) hue-rotate(180deg);
        }
        [data-mantine-color-scheme="dark"] .swagger-ui .info .title,
        [data-mantine-color-scheme="dark"] .swagger-ui .info .description,
        [data-mantine-color-scheme="dark"] .swagger-ui .opblock-tag,
        [data-mantine-color-scheme="dark"] .swagger-ui .opblock-summary-description {
          color: inherit;
        }
      `}</style>
    </Container>
  );
}
