"""paid plan types FREE/STARTER/PRO + instance upgrade/rebuild columns

Revision ID: a2b3c4d5e6f7
Revises: 1d0a44d56206
Create Date: 2026-07-06 00:00:00

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


revision: str = 'a2b3c4d5e6f7'
down_revision: Union[str, None] = '1d0a44d56206'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    """Add STARTER/PRO plan types, drop PAID_RESIDENT, add instance columns."""
    # 1. Migrate existing PAID_RESIDENT data to STARTER before touching the enum.
    #    SQLite can't ALTER enum in-place; PG/MySQL need value cleanup first.
    op.execute("UPDATE instances SET plan = 'STARTER' WHERE plan = 'PAID_RESIDENT'")
    op.execute("UPDATE users SET plan = 'STARTER' WHERE plan = 'PAID_RESIDENT'")

    # 2. Recreate the plantype enum with the three new tiers.
    #    batch_alter_table handles SQLite (recreate table) and PG/MySQL.
    with op.batch_alter_table('instances', schema=None) as batch_op:
        batch_op.alter_column(
            'plan',
            existing_type=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            type_=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )
    with op.batch_alter_table('users', schema=None) as batch_op:
        batch_op.alter_column(
            'plan',
            existing_type=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            type_=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )

    # 3. Add the five new instance columns.
    with op.batch_alter_table('instances', schema=None) as batch_op:
        batch_op.add_column(sa.Column('needs_upgrade', sa.Boolean(), nullable=True, server_default=sa.text('0')))
        batch_op.add_column(sa.Column('needs_rebuild', sa.Boolean(), nullable=True, server_default=sa.text('0')))
        batch_op.add_column(sa.Column('expected_plan', sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'), nullable=True))
        batch_op.add_column(sa.Column('storage_quota_gb', sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column('upgrade_retries', sa.Integer(), nullable=True, server_default=sa.text('0')))

    # 4. Backfill storage_quota_gb for existing FREE instances.
    op.execute("UPDATE instances SET storage_quota_gb = 10 WHERE storage_quota_gb IS NULL AND plan = 'FREE'")


def downgrade() -> None:
    """Revert to PAID_RESIDENT two-tier enum."""
    op.execute("UPDATE instances SET plan = 'PAID_RESIDENT' WHERE plan IN ('STARTER', 'PRO')")
    op.execute("UPDATE users SET plan = 'PAID_RESIDENT' WHERE plan IN ('STARTER', 'PRO')")
    with op.batch_alter_table('instances', schema=None) as batch_op:
        batch_op.drop_column('upgrade_retries')
        batch_op.drop_column('storage_quota_gb')
        batch_op.drop_column('expected_plan')
        batch_op.drop_column('needs_rebuild')
        batch_op.drop_column('needs_upgrade')
        batch_op.alter_column(
            'plan',
            existing_type=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            type_=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )
    with op.batch_alter_table('users', schema=None) as batch_op:
        batch_op.alter_column(
            'plan',
            existing_type=sa.Enum('FREE', 'STARTER', 'PRO', name='plantype'),
            type_=sa.Enum('FREE', 'PAID_RESIDENT', name='plantype'),
            existing_nullable=True,
            postgresql_using='plan::text',
        )
