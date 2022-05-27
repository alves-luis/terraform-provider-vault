package vault

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/vault/api"

	"github.com/hashicorp/terraform-provider-vault/internal/identity/entity"
	"github.com/hashicorp/terraform-provider-vault/util"
)

func identityEntityAliasResource() *schema.Resource {
	return &schema.Resource{
		CreateContext: identityEntityAliasCreate,
		UpdateContext: identityEntityAliasUpdate,
		ReadContext:   identityEntityAliasRead,
		DeleteContext: identityEntityAliasDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Name of the entity alias.",
			},

			"mount_accessor": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "Mount accessor to which this alias belongs toMount accessor to which this alias belongs to.",
			},

			"canonical_id": {
				Type:        schema.TypeString,
				Required:    true,
				Description: "ID of the entity to which this is an alias.",
			},
			"custom_metadata": {
				Type:        schema.TypeMap,
				Optional:    true,
				Description: "Custom metadata to be associated with this alias.",
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},
		},
	}
}

func identityEntityAliasCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	lock, unlock := getEntityLockFuncs(d, entity.RootAliasIDPath)
	lock()
	defer unlock()

	client := meta.(*api.Client)

	path := entity.RootAliasPath
	name := d.Get("name").(string)
	data := util.GetAPIRequestData(d, map[string]string{
		"name":            "",
		"mount_accessor":  "",
		"canonical_id":    "",
		"custom_metadata": "",
	})

	diags := diag.Diagnostics{}

	var duplicates []string

	mountAccessor := data["mount_accessor"].(string)
	aliases, err := entity.FindAliases(client, &entity.FindAliasParams{
		Name:          name,
		MountAccessor: mountAccessor,
	})
	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("Failed to get entity aliases by mount accessor, err=%s", err),
		})

		return diags
	}

	if len(aliases) > 0 {
		for _, alias := range aliases {
			duplicates = append(duplicates, alias.ID)
		}

		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary: fmt.Sprintf(
				"entity alias %q already exists for mount accessor %q, "+
					"ids=%q", name, mountAccessor, strings.Join(duplicates, ",")),
			Detail: "In the case where this error occurred during the creation of more than one alias, " +
				"it may be necessary to assign a unique alias name to each of affected resources and " +
				"then rerun the apply. After a successful apply the desired original alias names can then be " +
				"reassigned",
		})

		return diags
	}

	resp, err := client.Logical().Write(path, data)
	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary: fmt.Sprintf(
				"error writing entity alias to %q: %s", name, err),
		})

		return diags
	}

	if resp == nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary: fmt.Sprintf(
				"unexpected empty response during entity alias creation name=%q", name),
		})

		return diags

	}

	log.Printf("[DEBUG] Wrote entity alias %q", name)

	d.SetId(resp.Data["id"].(string))

	return identityEntityAliasRead(ctx, d, meta)
}

func identityEntityAliasUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	lock, unlock := getEntityLockFuncs(d, entity.RootAliasIDPath)
	lock()
	defer unlock()

	client := meta.(*api.Client)
	id := d.Id()

	log.Printf("[DEBUG] Updating entity alias %q", id)
	path := entity.JoinAliasID(id)

	diags := diag.Diagnostics{}

	data := util.GetAPIRequestData(d, map[string]string{
		"name":            "",
		"mount_accessor":  "",
		"canonical_id":    "",
		"custom_metadata": "",
	})
	if _, err := client.Logical().Write(path, data); err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("error updating entity alias %q: %s", id, err),
		})

		return diags
	}

	log.Printf("[DEBUG] Updated entity alias %q", id)

	return identityEntityAliasRead(ctx, d, meta)
}

func identityEntityAliasRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(*api.Client)
	id := d.Id()

	path := entity.JoinAliasID(id)

	diags := diag.Diagnostics{}

	log.Printf("[DEBUG] Reading entity alias %q from %q", id, path)
	resp, err := readEntity(client, path, d.IsNewResource())
	if err != nil {
		if isIdentityNotFoundError(err) {
			log.Printf("[WARN] entity alias %q not found, removing from state", id)
			d.SetId("")
			return diags
		}

		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("error reading entity alias %q: %s", id, err),
		})

		return diags
	}

	d.SetId(resp.Data["id"].(string))
	for _, k := range []string{"name", "mount_accessor", "canonical_id", "custom_metadata"} {
		if err := d.Set(k, resp.Data[k]); err != nil {
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  fmt.Sprintf("error setting state key %q on entity alias %q: err=%q", k, id, err),
			})

			return diags
		}
	}

	return diags
}

func identityEntityAliasDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	lock, unlock := getEntityLockFuncs(d, entity.RootAliasIDPath)
	lock()
	defer unlock()

	client := meta.(*api.Client)
	id := d.Id()

	path := entity.JoinAliasID(id)

	diags := diag.Diagnostics{}

	baseMsg := fmt.Sprintf("entity alias ID %q on mount_accessor %q", id, d.Get("mount_accessor"))
	log.Printf("[INFO] Deleting %s", baseMsg)
	_, err := client.Logical().Delete(path)
	if err != nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  fmt.Sprintf("failed deleting %s, err=%s", baseMsg, err),
		})
		return diags
	}
	log.Printf("[INFO] Successfully deleted %s", baseMsg)

	return diags
}

func getEntityLockFuncs(d *schema.ResourceData, root string) (func(), func()) {
	mountAccessor := d.Get("mount_accessor").(string)
	lockKey := strings.Join([]string{root, mountAccessor}, "/")
	lock := func() {
		vaultMutexKV.Lock(lockKey)
	}

	unlock := func() {
		vaultMutexKV.Unlock(lockKey)
	}
	return lock, unlock
}
