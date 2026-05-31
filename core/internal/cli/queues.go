package cli

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

func newQueuesCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queues",
		Short: "Manage task queues",
	}
	cmd.AddCommand(
		newQueuesCreateCommand(opts),
		newQueuesListCommand(opts),
		newQueuesGetCommand(opts),
		newQueuesRemoveCommand(opts),
		newQueuesPublishCommand(opts),
		newQueuesTaskCommand(opts),
	)

	return cmd
}

func newQueuesCreateCommand(opts *rootOptions) *cobra.Command {
	var key string

	cmd := &cobra.Command{
		Use:   "create [--key KEY]",
		Short: "Create a queue",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var queueKey *string
			if cmd.Flags().Changed("key") {
				queueKey = &key
			}

			created, err := apiClient(opts).CreateQueue(cmd.Context(), queue.CreateRequest{Key: queueKey})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique queue key")

	return cmd
}

func newQueuesListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List queues", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListQueues(cmd.Context(), limit, cursor)
	})
}

func newQueuesGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a queue", "queue ID", "queue key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetQueue(cmd.Context(), id, key)
	})
}

func newQueuesRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a queue", "queue ID", "queue key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveQueue(cmd.Context(), id, key)
	})
}

func newQueuesPublishCommand(opts *rootOptions) *cobra.Command {
	var (
		id         string
		key        string
		dataValue  string
		file       string
		retryValue string
	)

	cmd := &cobra.Command{
		Use:   "publish [--id ID | --key KEY] (--data JSON | --file PATH) [--retry JSON]",
		Short: "Publish a task to a queue",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			if (dataValue == "") == (file == "") {
				return errors.New("specify exactly one of --data or --file")
			}

			data := json.RawMessage(dataValue)

			if file != "" {
				contents, err := os.ReadFile(file) //nolint:gosec // CLI user explicitly chooses the task data file path.
				if err != nil {
					return err
				}

				data = json.RawMessage(contents)
			}

			var retry *queue.RetryOptions

			if retryValue != "" {
				var parsed queue.RetryOptions
				if err := json.Unmarshal([]byte(retryValue), &parsed); err != nil {
					return err
				}

				retry = &parsed
			}

			task, err := apiClient(opts).PublishQueueTask(cmd.Context(), id, key, queue.PublishRequest{Retry: retry, Data: data})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), task)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "queue ID")
	cmd.Flags().StringVar(&key, "key", "", "queue key")
	cmd.Flags().StringVar(&dataValue, "data", "", "inline task data JSON")
	cmd.Flags().StringVar(&file, "file", "", "task data JSON file")
	cmd.Flags().StringVar(&retryValue, "retry", "", "inline retry options JSON")

	return cmd
}

func newQueuesTaskCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "Inspect queue tasks",
	}
	cmd.AddCommand(newQueuesTaskGetCommand(opts))

	return cmd
}

func newQueuesTaskGetCommand(opts *rootOptions) *cobra.Command {
	var (
		id     string
		key    string
		taskID string
	)

	cmd := &cobra.Command{
		Use:   "get [--id ID | --key KEY] --task-id TASK_ID",
		Short: "Get a queue task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			if taskID == "" {
				return errors.New("specify --task-id")
			}

			task, err := apiClient(opts).GetQueueTask(cmd.Context(), id, key, taskID)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), task)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "queue ID")
	cmd.Flags().StringVar(&key, "key", "", "queue key")
	cmd.Flags().StringVar(&taskID, "task-id", "", "queue task ID")

	return cmd
}
