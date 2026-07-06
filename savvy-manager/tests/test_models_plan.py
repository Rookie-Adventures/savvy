from app.models import PlanType, Instance, InstanceStatus


def test_plan_type_has_three_tiers_no_paid_resident():
    members = {m.name for m in PlanType}
    assert members == {"FREE", "STARTER", "PRO"}
    assert not hasattr(PlanType, "PAID_RESIDENT")


def test_instance_has_new_columns():
    inst = Instance(
        instance_id="x", user_id="1", status=InstanceStatus.NOT_CREATED,
        plan=PlanType.STARTER, container_name="c", volume_name="v",
    )
    # New columns exist with defaults
    assert inst.needs_upgrade is False
    assert inst.needs_rebuild is False
    assert inst.expected_plan is None
    assert inst.storage_quota_gb is None
    assert inst.upgrade_retries == 0
