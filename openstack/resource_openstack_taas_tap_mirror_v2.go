package openstack

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/vpnaas/tapmirrors"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceTapMirrorV2() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceTapMirrorV2Create,
		ReadContext:   resourceTapMirrorV2Read,
		UpdateContext: resourceTapMirrorV2Update,
		DeleteContext: resourceTapMirrorV2Delete,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Update: schema.DefaultTimeout(10 * time.Minute),
			Delete: schema.DefaultTimeout(10 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
			},
			"tenant_id": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
			"project_id": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				Computed: true,
			},
			"port_id": {
				Type:     schema.TypeString,
				Computed: true,
				Optional: true,
				ForceNew: true,
			},
			"mirror_type": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					tapmirrors.MirrorTypeErspanv1, tapmirrors.MirrorTypeGre,
				}, false),
			},
			"remote_ip": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"directions": {
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: true,
				Elem: map[string]*schema.Schema{
					"in": {
						Type: schema.TypeInt,
						Optional: true,
					},
					"out": {
						Type: schema.TypeInt,
						Optional: true,
					},
				}
			},
		},
	}
}

func resourceTapMirrorV2Create(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	config := meta.(*Config)

	networkingClient, err := config.NetworkingV2Client(ctx, GetRegion(d, config))
	if err != nil {
		return diag.Errorf("Error creating OpenStack networking client: %s", err)
	}

	var createOpts TapMirrors.CreateOptsBuilder

	endpointType := resourceTapMirrorV2EndpointType(d.Get("type").(string))
	endpoints := expandToStringSlice(d.Get("endpoints").(*schema.Set).List())

	createOpts = TapMirrorCreateOpts{
		TapMirrors.CreateOpts{
			Name:        d.Get("name").(string),
			Description: d.Get("description").(string),
			TenantID:    d.Get("tenant_id").(string),
			Endpoints:   endpoints,
			Type:        endpointType,
		},
		MapValueSpecs(d),
	}

	log.Printf("[DEBUG] Create group: %#v", createOpts)

	group, err := TapMirrors.Create(ctx, networkingClient, createOpts).Extract()
	if err != nil {
		return diag.FromErr(err)
	}

	stateConf := &retry.StateChangeConf{
		Pending:    []string{"PENDING_CREATE"},
		Target:     []string{"ACTIVE"},
		Refresh:    waitForTapMirrorCreation(ctx, networkingClient, group.ID),
		Timeout:    d.Timeout(schema.TimeoutCreate),
		Delay:      0,
		MinTimeout: 2 * time.Second,
	}

	_, err = stateConf.WaitForStateContext(ctx)
	if err != nil {
		return diag.FromErr(err)
	}

	log.Printf("[DEBUG] TapMirror created: %#v", group)

	d.SetId(group.ID)

	return resourceTapMirrorV2Read(ctx, d, meta)
}

func resourceTapMirrorV2Read(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	log.Printf("[DEBUG] Retrieve information about group: %s", d.Id())

	config := meta.(*Config)

	networkingClient, err := config.NetworkingV2Client(ctx, GetRegion(d, config))
	if err != nil {
		return diag.Errorf("Error creating OpenStack networking client: %s", err)
	}

	group, err := TapMirrors.Get(ctx, networkingClient, d.Id()).Extract()
	if err != nil {
		return diag.FromErr(CheckDeleted(d, err, "group"))
	}

	log.Printf("[DEBUG] Read OpenStack Endpoint TapMirror %s: %#v", d.Id(), group)

	d.Set("name", group.Name)
	d.Set("description", group.Description)
	d.Set("tenant_id", group.TenantID)
	d.Set("type", group.Type)
	d.Set("endpoints", group.Endpoints)
	d.Set("region", GetRegion(d, config))

	return nil
}

func resourceTapMirrorV2Update(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	config := meta.(*Config)

	networkingClient, err := config.NetworkingV2Client(ctx, GetRegion(d, config))
	if err != nil {
		return diag.Errorf("Error creating OpenStack networking client: %s", err)
	}

	opts := TapMirrors.UpdateOpts{}

	var hasChange bool

	if d.HasChange("name") {
		name := d.Get("name").(string)
		opts.Name = &name
		hasChange = true
	}

	if d.HasChange("description") {
		description := d.Get("description").(string)
		opts.Description = &description
		hasChange = true
	}

	var updateOpts TapMirrors.UpdateOptsBuilder = opts

	log.Printf("[DEBUG] Updating endpoint group with id %s: %#v", d.Id(), updateOpts)

	if hasChange {
		group, err := TapMirrors.Update(ctx, networkingClient, d.Id(), updateOpts).Extract()
		if err != nil {
			return diag.FromErr(err)
		}

		stateConf := &retry.StateChangeConf{
			Pending:    []string{"PENDING_UPDATE"},
			Target:     []string{"UPDATED"},
			Refresh:    waitForTapMirrorUpdate(ctx, networkingClient, group.ID),
			Timeout:    d.Timeout(schema.TimeoutCreate),
			Delay:      0,
			MinTimeout: 2 * time.Second,
		}

		_, err = stateConf.WaitForStateContext(ctx)
		if err != nil {
			return diag.FromErr(err)
		}

		log.Printf("[DEBUG] Updated group with id %s", d.Id())
	}

	return resourceTapMirrorV2Read(ctx, d, meta)
}

func resourceTapMirrorV2Delete(ctx context.Context, d *schema.ResourceData, meta any) diag.Diagnostics {
	log.Printf("[DEBUG] Destroy group: %s", d.Id())

	config := meta.(*Config)

	networkingClient, err := config.NetworkingV2Client(ctx, GetRegion(d, config))
	if err != nil {
		return diag.Errorf("Error creating OpenStack networking client: %s", err)
	}

	err = TapMirrors.Delete(ctx, networkingClient, d.Id()).Err
	if err != nil {
		return diag.FromErr(err)
	}

	stateConf := &retry.StateChangeConf{
		Pending:    []string{"DELETING"},
		Target:     []string{"DELETED"},
		Refresh:    waitForTapMirrorDeletion(ctx, networkingClient, d.Id()),
		Timeout:    d.Timeout(schema.TimeoutDelete),
		Delay:      0,
		MinTimeout: 2 * time.Second,
	}

	_, err = stateConf.WaitForStateContext(ctx)

	return diag.FromErr(err)
}

func waitForTapMirrorDeletion(ctx context.Context, networkingClient *gophercloud.ServiceClient, id string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		group, err := TapMirrors.Get(ctx, networkingClient, id).Extract()
		log.Printf("[DEBUG] Got group %s => %#v", id, group)

		if err != nil {
			if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
				log.Printf("[DEBUG] TapMirror %s is actually deleted", id)

				return "", "DELETED", nil
			}

			return nil, "", fmt.Errorf("Unexpected error: %w", err)
		}

		log.Printf("[DEBUG] TapMirror %s deletion is pending", id)

		return group, "DELETING", nil
	}
}

func waitForTapMirrorCreation(ctx context.Context, networkingClient *gophercloud.ServiceClient, id string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		group, err := TapMirrors.Get(ctx, networkingClient, id).Extract()
		if err != nil {
			return "", "PENDING_CREATE", nil
		}

		return group, "ACTIVE", nil
	}
}

func waitForTapMirrorUpdate(ctx context.Context, networkingClient *gophercloud.ServiceClient, id string) retry.StateRefreshFunc {
	return func() (any, string, error) {
		group, err := TapMirrors.Get(ctx, networkingClient, id).Extract()
		if err != nil {
			return "", "PENDING_UPDATE", nil
		}

		return group, "UPDATED", nil
	}
}

func resourceTapMirrorV2EndpointType(epType string) TapMirrors.EndpointType {
	var et TapMirrors.EndpointType

	switch epType {
	case "subnet":
		et = TapMirrors.TypeSubnet
	case "cidr":
		et = TapMirrors.TypeCIDR
	case "vlan":
		et = TapMirrors.TypeVLAN
	case "router":
		et = TapMirrors.TypeRouter
	case "network":
		et = TapMirrors.TypeNetwork
	}

	return et
}
