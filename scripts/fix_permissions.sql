-- Grant necessary permissions to daz_user
GRANT CREATE ON SCHEMA public TO daz_user;
GRANT ALL ON ALL TABLES IN SCHEMA public TO daz_user;
GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO daz_user;
GRANT ALL ON ALL FUNCTIONS IN SCHEMA public TO daz_user;