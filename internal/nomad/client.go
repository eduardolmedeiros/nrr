// Package nomad handles Nomad API interaction: discovering jobs, task groups,
// tasks, their current declared resource specs, and running allocation IDs.
package nomad

import (
	"context"
	"fmt"

	nomadapi "github.com/hashicorp/nomad/api"
)

// TaskSpec holds the identifying information and current resource spec for
// a single Nomad task. This is the unit that NRR generates recommendations for.
type TaskSpec struct {
	Namespace string
	Job       string
	Group     string
	Task      string

	// Current declared resources (what the job file says).
	CPUMHz   int // CPU in MHz as declared in the job spec
	MemoryMB int // Memory in MB as declared in the job spec

	// AllocIDs contains the IDs of currently running allocations for this
	// task's group. Used by the cAdvisor adapter to filter metrics by
	// container_label_com_hashicorp_nomad_alloc_id when job/task name labels
	// are not present.
	AllocIDs []string
}

// Client wraps the Nomad API client.
type Client struct {
	api *nomadapi.Client
}

// NewClient creates a new Nomad API client pointing at the given address.
func NewClient(address string) (*Client, error) {
	cfg := nomadapi.DefaultConfig()
	cfg.Address = address

	client, err := nomadapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating Nomad client: %w", err)
	}
	return &Client{api: client}, nil
}

// DiscoverTasks lists all running tasks across all jobs in the given namespace,
// optionally filtered to a single job name. It returns each task's current
// declared CPU (MHz) and memory (MB) specs, plus the running allocation IDs.
func (c *Client) DiscoverTasks(ctx context.Context, namespace, jobFilter string) ([]TaskSpec, error) {
	qOpts := &nomadapi.QueryOptions{Namespace: namespace}
	qOpts = qOpts.WithContext(ctx)

	if jobFilter != "" {
		job, _, err := c.api.Jobs().Info(jobFilter, qOpts)
		if err != nil {
			return nil, fmt.Errorf("fetching job %q: %w", jobFilter, err)
		}
		return c.tasksFromJob(ctx, job, namespace, qOpts)
	}

	jobStubs, _, err := c.api.Jobs().List(qOpts)
	if err != nil {
		return nil, fmt.Errorf("listing jobs: %w", err)
	}

	var tasks []TaskSpec
	for _, stub := range jobStubs {
		if stub.Status != "running" {
			continue
		}
		job, _, err := c.api.Jobs().Info(stub.ID, qOpts)
		if err != nil {
			return nil, fmt.Errorf("fetching job %q: %w", stub.ID, err)
		}
		jobTasks, err := c.tasksFromJob(ctx, job, namespace, qOpts)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, jobTasks...)
	}

	return tasks, nil
}

// tasksFromJob extracts TaskSpec entries from a fully-fetched Nomad job,
// including the running allocation IDs for each task group.
func (c *Client) tasksFromJob(ctx context.Context, job *nomadapi.Job, namespace string, qOpts *nomadapi.QueryOptions) ([]TaskSpec, error) {
	if job == nil || job.TaskGroups == nil {
		return nil, nil
	}

	ns := namespace
	if job.Namespace != nil && *job.Namespace != "" {
		ns = *job.Namespace
	}

	// Fetch running allocations for this job to populate AllocIDs.
	// alloc_id is per task-group, not per task, so we build a map:
	// group name → []alloc_id
	groupAllocs, err := c.runningAllocsByGroup(ctx, *job.ID, qOpts)
	if err != nil {
		// Non-fatal — we can still recommend without alloc IDs,
		// cAdvisor adapter will just return no data for this job.
		groupAllocs = map[string][]string{}
	}

	var tasks []TaskSpec
	for _, group := range job.TaskGroups {
		if group == nil {
			continue
		}
		allocIDs := groupAllocs[*group.Name]

		for _, task := range group.Tasks {
			if task == nil {
				continue
			}

			spec := TaskSpec{
				Namespace: ns,
				Job:       *job.ID,
				Group:     *group.Name,
				Task:      task.Name,
				AllocIDs:  allocIDs,
			}

			if task.Resources != nil {
				if task.Resources.CPU != nil {
					spec.CPUMHz = *task.Resources.CPU
				}
				if task.Resources.MemoryMB != nil {
					spec.MemoryMB = *task.Resources.MemoryMB
				}
			}

			tasks = append(tasks, spec)
		}
	}

	return tasks, nil
}

// runningAllocsByGroup returns a map of group name → running alloc IDs.
func (c *Client) runningAllocsByGroup(ctx context.Context, jobID string, qOpts *nomadapi.QueryOptions) (map[string][]string, error) {
	allocs, _, err := c.api.Jobs().Allocations(jobID, false, qOpts)
	if err != nil {
		return nil, fmt.Errorf("listing allocations for job %q: %w", jobID, err)
	}

	result := map[string][]string{}
	for _, alloc := range allocs {
		if alloc.ClientStatus != "running" {
			continue
		}
		result[alloc.TaskGroup] = append(result[alloc.TaskGroup], alloc.ID)
	}
	return result, nil
}
