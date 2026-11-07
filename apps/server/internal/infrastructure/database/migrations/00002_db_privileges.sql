-- +goose Up
-- +goose StatementBegin
do $$ begin
  execute format('revoke connect, temporary on database %I from public', current_database());
  execute format('grant connect on database %I to mypark_app, mypark_migrator', current_database());
end $$;
-- +goose StatementEnd
