package storage

type scanner interface{ Scan(...any) error }
