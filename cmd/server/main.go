package main

import (
 "context"
 "database/sql"
 "log/slog"
 "net/http"
 "os"
 "os/signal"
 "syscall"
 "time"

 "github.com/WesleyCastilho/valto-notification/internal/notification"
 _ "github.com/jackc/pgx/v5/stdlib"
)

func main(){
 logger:=slog.New(slog.NewJSONHandler(os.Stdout,nil));slog.SetDefault(logger)
 cfg,err:=notification.LoadConfig();if err!=nil{logger.Error("invalid_configuration","reason",err.Error());os.Exit(1)}
 db,err:=sql.Open("pgx",cfg.DatabaseURL);if err!=nil{logger.Error("database_configuration_invalid");os.Exit(1)};defer db.Close();db.SetMaxOpenConns(16);db.SetMaxIdleConns(4);db.SetConnMaxLifetime(30*time.Minute)
 ctx,stop:=signal.NotifyContext(context.Background(),os.Interrupt,syscall.SIGTERM);defer stop()
 startup,cancel:=context.WithTimeout(ctx,30*time.Second);store:=&notification.Store{DB:db};err=db.PingContext(startup);if err==nil{err=store.Migrate(startup)};cancel();if err!=nil{logger.Error("database_startup_failed");os.Exit(1)}
 api:=&notification.API{Store:store,Tenants:cfg.Tenants,DeliveryEnabled:cfg.DeliveryEnabled,WorkerOnly:cfg.Mode=="worker",Log:logger}
 server:=&http.Server{Addr:":"+cfg.Port,Handler:api.Handler(),ReadHeaderTimeout:5*time.Second,ReadTimeout:20*time.Second,WriteTimeout:30*time.Second,IdleTimeout:60*time.Second}
 workerDone:=make(chan struct{});if cfg.Mode!="api"{worker:=notification.Worker{Store:store,Tenants:cfg.Tenants,Interval:cfg.Interval,Enabled:cfg.DeliveryEnabled,Log:logger};go func(){defer close(workerDone);worker.Run(ctx)}()}else{close(workerDone)}
 errs:=make(chan error,1);go func(){errs<-server.ListenAndServe()}();logger.Info("server_started","port",cfg.Port,"mode",cfg.Mode,"delivery_enabled",cfg.DeliveryEnabled)
 select{case<-ctx.Done():case err:=<-errs:if err!=http.ErrServerClosed{logger.Error("http_server_failed")};stop()}
 shutdown,finish:=context.WithTimeout(context.Background(),25*time.Second);defer finish();_ = server.Shutdown(shutdown)
 select{case<-workerDone:case<-shutdown.Done():logger.Warn("shutdown_deadline_reached")}
}
