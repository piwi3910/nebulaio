import { useMemo } from 'react';
import {
  Stack,
  Textarea,
  Text,
  Alert,
  Group,
  Button,
  Menu,
  Paper,
  Code,
} from '@mantine/core';
import { IconAlertCircle, IconTemplate, IconCheck } from '@tabler/icons-react';

interface PolicyEditorProps {
  value: string;
  onChange: (value: string) => void;
  error?: string;
  readOnly?: boolean;
}

// Policy templates
const policyTemplates = {
  readOnly: {
    name: 'Read Only',
    description: 'Allow read access to all buckets',
    document: {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['s3:GetObject', 's3:ListBucket', 's3:GetBucketLocation'],
          Resource: ['arn:aws:s3:::*', 'arn:aws:s3:::*/*'],
        },
      ],
    },
  },
  fullAccess: {
    name: 'Full Access',
    description: 'Allow full access to all S3 operations',
    document: {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['s3:*'],
          Resource: ['arn:aws:s3:::*', 'arn:aws:s3:::*/*'],
        },
      ],
    },
  },
  bucketAdmin: {
    name: 'Bucket Admin',
    description: 'Allow full access to a specific bucket',
    document: {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['s3:*'],
          Resource: ['arn:aws:s3:::my-bucket', 'arn:aws:s3:::my-bucket/*'],
        },
      ],
    },
  },
  writeOnly: {
    name: 'Write Only',
    description: 'Allow write access but not read',
    document: {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['s3:PutObject', 's3:DeleteObject'],
          Resource: ['arn:aws:s3:::*/*'],
        },
      ],
    },
  },
  listOnly: {
    name: 'List Only',
    description: 'Allow listing buckets and objects only',
    document: {
      Version: '2012-10-17',
      Statement: [
        {
          Effect: 'Allow',
          Action: ['s3:ListBucket', 's3:ListAllMyBuckets', 's3:GetBucketLocation'],
          Resource: ['arn:aws:s3:::*'],
        },
      ],
    },
  },
};

export function PolicyEditor({ value, onChange, error, readOnly = false }: PolicyEditorProps) {
  // Derive validation state from value using useMemo (no useEffect needed)
  const { syntaxError, isValid } = useMemo(() => {
    if (!value.trim()) {
      return { syntaxError: null, isValid: false };
    }

    try {
      const parsed = JSON.parse(value);

      // Basic policy structure validation
      if (!parsed.Version) {
        return { syntaxError: 'Policy must include a "Version" field', isValid: false };
      }

      if (!parsed.Statement || !Array.isArray(parsed.Statement)) {
        return { syntaxError: 'Policy must include a "Statement" array', isValid: false };
      }

      for (let i = 0; i < parsed.Statement.length; i++) {
        const stmt = parsed.Statement[i];
        if (!stmt.Effect || !['Allow', 'Deny'].includes(stmt.Effect)) {
          return { syntaxError: `Statement ${i + 1}: Effect must be "Allow" or "Deny"`, isValid: false };
        }
        if (!stmt.Action) {
          return { syntaxError: `Statement ${i + 1}: Missing "Action" field`, isValid: false };
        }
        if (!stmt.Resource) {
          return { syntaxError: `Statement ${i + 1}: Missing "Resource" field`, isValid: false };
        }
      }

      return { syntaxError: null, isValid: true };
    } catch (e) {
      if (e instanceof SyntaxError) {
        return { syntaxError: `JSON syntax error: ${e.message}`, isValid: false };
      }
      return { syntaxError: 'Invalid JSON', isValid: false };
    }
  }, [value]);

  const formatJson = () => {
    try {
      const parsed = JSON.parse(value);
      onChange(JSON.stringify(parsed, null, 2));
    } catch {
      // Can't format invalid JSON
    }
  };

  const applyTemplate = (templateKey: keyof typeof policyTemplates) => {
    const template = policyTemplates[templateKey];
    onChange(JSON.stringify(template.document, null, 2));
  };

  return (
    <Stack gap="sm">
      {!readOnly && (
        <Group justify="space-between">
          <Menu shadow="md" width={250}>
            <Menu.Target>
              <Button variant="light" size="xs" leftSection={<IconTemplate size={14} />}>
                Templates
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>Insert Policy Template</Menu.Label>
              {Object.entries(policyTemplates).map(([key, template]) => (
                <Menu.Item
                  key={key}
                  onClick={() => applyTemplate(key as keyof typeof policyTemplates)}
                >
                  <div>
                    <Text size="sm" fw={500}>
                      {template.name}
                    </Text>
                    <Text size="xs" c="dimmed">
                      {template.description}
                    </Text>
                  </div>
                </Menu.Item>
              ))}
            </Menu.Dropdown>
          </Menu>

          <Group gap="xs">
            {isValid && (
              <Text size="xs" c="green">
                <Group gap={4}>
                  <IconCheck size={14} />
                  Valid JSON
                </Group>
              </Text>
            )}
            <Button variant="subtle" size="xs" onClick={formatJson} disabled={!value}>
              Format
            </Button>
          </Group>
        </Group>
      )}

      <Textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={JSON.stringify(policyTemplates.readOnly.document, null, 2)}
        minRows={15}
        maxRows={25}
        autosize
        styles={{
          input: {
            fontFamily: 'monospace',
            fontSize: '13px',
          },
        }}
        error={error}
        readOnly={readOnly}
      />

      {syntaxError && (
        <Alert
          icon={<IconAlertCircle size={16} />}
          title="Validation Error"
          color="red"
          variant="light"
        >
          {syntaxError}
        </Alert>
      )}

      {!readOnly && (
        <Paper withBorder p="sm" bg="gray.0">
          <Text size="xs" c="dimmed" mb="xs" fw={500}>
            Available S3 Actions:
          </Text>
          <Group gap="xs">
            {[
              's3:GetObject',
              's3:PutObject',
              's3:DeleteObject',
              's3:ListBucket',
              's3:CreateBucket',
              's3:DeleteBucket',
              's3:GetBucketLocation',
              's3:*',
            ].map((action) => (
              <Code key={action} style={{ fontSize: '11px' }}>
                {action}
              </Code>
            ))}
          </Group>
        </Paper>
      )}
    </Stack>
  );
}
